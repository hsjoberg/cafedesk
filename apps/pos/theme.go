package main

import (
	_ "embed"
	"fmt"

	"github.com/egoist/mygo/ui"
)

// The tokens of docs/design.md.

//go:embed assets/fonts/ReadexPro-Regular.ttf
var fontRegular []byte

//go:embed assets/fonts/ReadexPro-Medium.ttf
var fontMedium []byte

//go:embed assets/fonts/ReadexPro-Bold.ttf
var fontBold []byte

// fontFamily is Readex Pro, which covers Latin and Arabic in one design.
const fontFamily = "Readex Pro"

func registerFonts() error {
	for _, f := range [][]byte{fontRegular, fontMedium, fontBold} {
		if err := ui.RegisterFont(f, fontFamily); err != nil {
			return fmt.Errorf("register %s: %w", fontFamily, err)
		}
	}
	return nil
}

var (
	colConcrete       = ui.Hex("#CDD2C9") // the app's ground
	colTile           = ui.Hex("#F4F5F1") // tiles and buttons
	colTilePressed    = ui.Hex("#E2E6DE")
	colPaper          = ui.Hex("#FFFFFF") // the receipt strip only
	colInk            = ui.Hex("#213A31") // all text
	colInkMuted       = ui.Hex("#56675F")
	colRule           = ui.Hex("#AEB7AA") // dashed rules on the paper
	colSaffron        = ui.Hex("#F2A20C") // money moves only
	colSaffronPressed = ui.Hex("#D58C00")
	colChili          = ui.Hex("#C23B2E") // destructive only
	colChiliPressed   = ui.Hex("#A33024")
	colInkPressed     = ui.Hex("#152821")
	colScrim          = ui.RGBA(0x21, 0x3a, 0x31, 0.18) // the paper's shadow
	colScrimDeep      = ui.RGBA(0x21, 0x3a, 0x31, 0.45) // behind sheets over the whole window
)

// categoryColor is a category's signage band, from the menu's hex color.
func categoryColor(c Category) ui.Color {
	if len(c.Color) == 7 && c.Color[0] == '#' {
		return ui.Hex(c.Color)
	}
	return colInkMuted
}

// The type scale.
const (
	fsMeta   = 14
	fsBody   = 16
	fsItem   = 18
	fsPrice  = 21
	fsKey    = 24
	fsTotal  = 36
	fsChange = 48

	wRegular = 400
	wMedium  = 500
	wBold    = 700
)

// posTheme is the window's theme: the system's light theme in the POS's
// colors and font, roomier for touch, with ink (not saffron) as the accent
// so saffron stays for money.
func posTheme() *ui.Theme {
	t := ui.LightTheme().WithAccent(colInk)
	t.Background = colConcrete
	t.Surface = colTile
	t.SurfaceHover = colTile
	t.SurfacePressed = colTilePressed
	t.Text = colInk
	t.TextMuted = colInkMuted
	t.Border = colRule
	t.Danger = colChili
	t.Font = fontFamily
	t.FontSize = fsBody
	t.Radius = 4
	t.Spacing = 5
	t.FocusRingWidth = 3
	return t
}
