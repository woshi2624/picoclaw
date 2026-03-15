//go:build embedded_binary

package main

import _ "embed"

//go:embed embedded/picoclaw.bin
var embeddedPicoclawBin []byte
