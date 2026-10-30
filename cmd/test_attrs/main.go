package main

import (
    "fmt"
    "github.com/GenshIv/makoshop/internal/attrs"
)

func main() {
    // Test SplitValues with various inputs
    tests := []string{
        "czarny",
        "czarny, biały, czerwony",
        "5 W",
        "5 W, 10 W",
        "ISOFIX",
        "15\"",
        "21L",
        "Wi-Fi, Bluetooth, LAN 10/100/1000 Mbps",
        "minijack 3,5 mm (audio), HDMI, RJ-45, USB 3.2 typ A Gen 1, Thunderbolt",
    }
    
    for _, t := range tests {
        split := attrs.SplitValues(t)
        fmt.Printf("Input: %q\n", t)
        fmt.Printf("  Split: %v\n", split)
        fmt.Println()
    }
}
