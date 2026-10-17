package main

import _ "embed"

//go:generate go run ./tools/icon
//go:generate go run github.com/tc-hib/go-winres@v0.3.3 make --in assets/winres.json --out rsrc --arch amd64,arm64

//go:embed assets/icon.png
var appIconPNG []byte
