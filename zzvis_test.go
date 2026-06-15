package main

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/charmbracelet/bubbles/list"
)

func TestVisualGutter(t *testing.T) {
	d := newItemDelegate()
	items := []list.Item{
		item{number: 11, title: "#11 first issue alpha", type_: "issue"},
		item{number: 12, title: "#12 second issue beta", type_: "issue"},
	}
	l := list.New(items, d, 40, 24)
	for sel := 0; sel < 2; sel++ {
		l.Select(sel)
		for i := range items {
			var buf bytes.Buffer
			d.Render(&buf, l, i, items[i])
			fmt.Printf("sel=%d row=%d -> %q\n", sel, i, stripANSI(buf.String()))
		}
	}
}
