package main

import "fyne.io/fyne/v2"

// The application icon is self-contained vector artwork, so portable builds
// do not depend on a separate file beside the executable.
const vaulticIconSVG = "<svg xmlns=\"http://www.w3.org/2000/svg\" width=\"128\" height=\"128\" viewBox=\"0 0 128 128\">\n<defs><linearGradient id=\"a\" x1=\"0\" y1=\"0\" x2=\"1\" y2=\"1\"><stop stop-color=\"#7FF4D9\"/><stop offset=\"1\" stop-color=\"#38BDB4\"/></linearGradient></defs>\n<rect width=\"128\" height=\"128\" rx=\"30\" fill=\"#101C30\"/>\n<path d=\"M64 13 107 32v31c0 30-16 47-43 54C37 110 21 93 21 63V32L64 13Z\" fill=\"none\" stroke=\"url(#a)\" stroke-width=\"6\"/>\n<path d=\"m40 48 24 43 24-43\" fill=\"none\" stroke=\"#EAF8F6\" stroke-width=\"9\" stroke-linecap=\"round\" stroke-linejoin=\"round\"/>\n<circle cx=\"64\" cy=\"30\" r=\"4\" fill=\"#54E2C1\"/>\n</svg>"

func vaulticAppIcon() fyne.Resource {
    return fyne.NewStaticResource("vaultic.svg", []byte(vaulticIconSVG))
}
