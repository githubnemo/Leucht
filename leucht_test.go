package main

import (
	"testing"
)

func TestColorFromLoad(t *testing.T) {
	c := ColorFromLoad(0)

	if ! (c.R == 0 && c.G == 0 && c.B == 255) {
		t.Fatal("Color not blue with zero load")
	}

	// Highest load with actual processors
	c1 := ColorFromLoad(50)

	// HT load
	c2 := ColorFromLoad(100)

	if c1.R >= c2.R {
		t.Fatal("HT load has not or negatively affected load.")
	}
}
