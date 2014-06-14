package main

import (
	"encoding/xml"
	"flag"
	"fmt"
	"github.com/PuerkitoBio/goquery"
	"code.google.com/p/go-charset/charset"
	_ "code.google.com/p/go-charset/data"
	"io/ioutil"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

var FlagURL = flag.String("url", "http://localhost/ganglia/", "URL to ganglia")

var FlagPiURL = flag.String("piurl", "http://alarmpi.local:1337", "URL to color pi")

var FlagInterval = flag.Uint("interval", 1, "In seconds when to fetch load.")

var FlagGMonHost = flag.String("gmonhost", "localhost:8649", "Ganglia gmond host")

type RGB struct {
	R, G, B uint8
}

func (c RGB) String() string {
	return fmt.Sprintf("#%02x%02x%02x", c.R, c.G, c.B)
}

type LoadLoader struct {
	sync.RWMutex
	currentLoad uint
	channels    []chan uint
}

func (c *LoadLoader) LoadPeriodically(d time.Duration) {
	go c.loader(d)
	c.LoadOnce()
}

func (c *LoadLoader) LoadOnce() uint {
	load := c.fetchLoad()

	c.Lock()
	c.currentLoad = load
	c.Unlock()

	for _, ch := range c.channels {
		go func(load uint) { ch <- load }(c.currentLoad)
	}

	return c.currentLoad
}

func (c *LoadLoader) Chan() chan uint {
	ch := make(chan uint)
	c.channels = append(c.channels, ch)
	return ch
}

func (c *LoadLoader) CurrentLoad() uint {
	c.RLock()
	defer c.RUnlock()
	return c.currentLoad
}

func (c *LoadLoader) loader(d time.Duration) {
	for {
		c.LoadOnce()
		<-time.After(d)
	}
}

func (c *LoadLoader) fetchLoad() uint {
	return c.fetchLoadGanglia()
}

func (c *LoadLoader) fetchLoadGanglia() uint {
	conn, err := net.Dial("tcp", *FlagGMonHost)

	if err != nil {
		log.Println("Error connecting to ganglia:", err)
		return 0
	}

	defer conn.Close()

	gangliaData := struct{
		Cluster struct {
			Name string `xml:"NAME,attr"`
			Hosts []struct {
				Name string `xml:"NAME,attr"`
				Metrics []struct {
					Name string `xml:"NAME,attr"`
					Value string `xml:"VAL,attr"`
					Type string `xml:"TYPE,attr"`
				} `xml:"METRIC"`
			} `xml:"HOST"`
		} `xml:"CLUSTER"`
	}{}

	dec := xml.NewDecoder(conn)
	dec.CharsetReader = charset.NewReader
	err = dec.Decode(&gangliaData)

	if err != nil {
		log.Println("Error parsing ganglia XML:", err)
		return 0
	}

	var hostCPU float64
	var numNodes uint
	for _, host := range gangliaData.Cluster.Hosts {
		if strings.HasPrefix(host.Name, "yashik") {
			numNodes++
		}
		for _, metric := range host.Metrics {
			switch metric.Name {
			case "cpu_user":
				fallthrough
			case "cpu_system":
				val, err := strconv.ParseFloat(metric.Value, 64)
				if err != nil {
					log.Println("Error while parsing", metric.Name, ":", err)
					continue
				}
				hostCPU += val
			}
		}
	}

	return uint(hostCPU / float64(numNodes))
}

func (c *LoadLoader) fetchLoadWeb() uint {
	doc, err := goquery.NewDocument(*FlagURL)

	if err != nil {
		log.Println("Error fetching ganglia page:", err)
		return 0
	}

	selection := doc.Find("form > table").Eq(1).Find("table tr:nth-child(5) td b")
	split := strings.Split(selection.Text(), ", ")
	load, err := strconv.ParseUint(strings.Trim(split[2],"%"), 10, 32)

	if err != nil {
		log.Println("Error parsing load:", split[0], err)
		return 0
	}

	return uint(load)
}

func ColorFromLoad(load uint) RGB {
	overhang := uint(0)

	processorWeight := 95. / 50
	hyperthreadWeight := 5. / 50

	if load > 50 {
		overhang = load - 50
		load -= 50
	}

	multiplier := float64(load)*processorWeight + float64(overhang)*hyperthreadWeight
	multiplier /= 100

	return RGB{
		uint8(0xFF * multiplier),
		0,
		uint8(0xFF * (1 - multiplier)),
	}
}

func FetchCurrentColor() (c RGB) {
	resp, err := http.Get("http://alarmpi.local:1337/color")
	if err != nil {
		return
	}
	defer resp.Body.Close()
	body, err := ioutil.ReadAll(resp.Body)
	_, err = fmt.Sscanf(string(body), "#%2x%2x%2x", &c.R, &c.G, &c.B)
	if err != nil {
		log.Println(err)
	}
	return
}

func SendColor(c RGB) {
	url := fmt.Sprintf(*FlagPiURL+"/do?action=set&r=%d&g=%d&b=%d", c.R, c.G, c.B)

	resp, err := http.Get(url)
	if err != nil {
		log.Println(err)
		return
	}
	defer resp.Body.Close()
}

func FadeColor(from, to RGB) {
	stepper := func(a, b uint8) uint8 {
		if a < b {
			return a + 1
		} else if a > b {
			return a - 1
		} else {
			return b
		}
	}

	for from != to {
		from.R = stepper(from.R, to.R)
		from.G = stepper(from.G, to.G)
		from.B = stepper(from.B, to.B)

		SendColor(from)
	}
}

func main() {
	flag.Parse()

	loadLoader := &LoadLoader{}
	loadLoader.LoadPeriodically(time.Duration(*FlagInterval) * time.Second)

	currentColor := FetchCurrentColor()

	for currentLoad := range loadLoader.Chan() {
		loadColor := ColorFromLoad(currentLoad)

		fmt.Println("Current load:", currentLoad)
		fmt.Println("Resulting color:", loadColor)

		FadeColor(currentColor, loadColor)

		currentColor = loadColor
	}
}
