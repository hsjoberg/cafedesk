//go:build windows

package main

import (
	"errors"
	"fmt"
	"image"
	"runtime"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Printing goes through the Windows print spooler, so any printer Windows
// knows works, over USB or the network. Two ways:
//
//   - printRaw sends bytes the printer reads itself, as ESC/POS for a
//     receipt printer. The driver passes them through untouched.
//   - printText has the printer's driver draw the text, for printers
//     that take no raw bytes.

var (
	winspool = windows.NewLazySystemDLL("winspool.drv")
	gdi32    = windows.NewLazySystemDLL("gdi32.dll")

	procEnumPrintersW      = winspool.NewProc("EnumPrintersW")
	procGetDefaultPrinterW = winspool.NewProc("GetDefaultPrinterW")
	procOpenPrinterW       = winspool.NewProc("OpenPrinterW")
	procClosePrinter       = winspool.NewProc("ClosePrinter")
	procStartDocPrinterW   = winspool.NewProc("StartDocPrinterW")
	procEndDocPrinter      = winspool.NewProc("EndDocPrinter")
	procStartPagePrinter   = winspool.NewProc("StartPagePrinter")
	procEndPagePrinter     = winspool.NewProc("EndPagePrinter")
	procWritePrinter       = winspool.NewProc("WritePrinter")

	procCreateDCW     = gdi32.NewProc("CreateDCW")
	procDeleteDC      = gdi32.NewProc("DeleteDC")
	procStartDocW     = gdi32.NewProc("StartDocW")
	procEndDoc        = gdi32.NewProc("EndDoc")
	procAbortDoc      = gdi32.NewProc("AbortDoc")
	procStartPage     = gdi32.NewProc("StartPage")
	procEndPage       = gdi32.NewProc("EndPage")
	procTextOutW      = gdi32.NewProc("TextOutW")
	procGetDeviceCaps = gdi32.NewProc("GetDeviceCaps")
	procCreateFontW   = gdi32.NewProc("CreateFontW")
	procSelectObject  = gdi32.NewProc("SelectObject")
	procDeleteObject  = gdi32.NewProc("DeleteObject")
	procStretchDIBits = gdi32.NewProc("StretchDIBits")
)

const (
	printerEnumLocal       = 0x2
	printerEnumConnections = 0x4
	logPixelsY             = 90
	logPixelsX             = 88
	horzRes                = 8
	dibRGBColors           = 0
	srcCopy                = 0x00CC0020
	printerDots            = 203 // a receipt printer's dots an inch, as pictures are drawn for
	defaultCharset         = 1
	fixedPitchModern       = 0x1 | 0x30 // FIXED_PITCH | FF_MODERN
)

// printerInfo4 is PRINTER_INFO_4W.
type printerInfo4 struct {
	PrinterName *uint16
	ServerName  *uint16
	Attributes  uint32
}

// listPrinters returns the names of the printers Windows has.
func listPrinters() ([]string, error) {
	flags := uintptr(printerEnumLocal | printerEnumConnections)
	var needed, returned uint32
	procEnumPrintersW.Call(flags, 0, 4, 0, 0,
		uintptr(unsafe.Pointer(&needed)), uintptr(unsafe.Pointer(&returned)))
	if needed == 0 {
		return nil, nil
	}
	// A []uint64 keeps the structs Windows writes aligned.
	buf := make([]uint64, (needed+7)/8)
	r, _, err := procEnumPrintersW.Call(flags, 0, 4,
		uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)*8),
		uintptr(unsafe.Pointer(&needed)), uintptr(unsafe.Pointer(&returned)))
	if r == 0 {
		return nil, fmt.Errorf("EnumPrinters: %w", err)
	}
	infos := unsafe.Slice((*printerInfo4)(unsafe.Pointer(&buf[0])), returned)
	names := make([]string, 0, returned)
	for _, in := range infos {
		names = append(names, windows.UTF16PtrToString(in.PrinterName))
	}
	runtime.KeepAlive(buf)
	return names, nil
}

// defaultPrinter returns the name of Windows's default printer.
func defaultPrinter() (string, error) {
	var n uint32
	procGetDefaultPrinterW.Call(0, uintptr(unsafe.Pointer(&n)))
	if n == 0 {
		return "", errors.New("no default printer")
	}
	buf := make([]uint16, n)
	r, _, err := procGetDefaultPrinterW.Call(uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&n)))
	if r == 0 {
		return "", fmt.Errorf("GetDefaultPrinter: %w", err)
	}
	return windows.UTF16ToString(buf), nil
}

// docInfo1 is DOC_INFO_1W.
type docInfo1 struct {
	DocName    *uint16
	OutputFile *uint16
	Datatype   *uint16
}

// printRaw sends data to the printer as it is, as a RAW print job.
func printRaw(printer, doc string, data []byte) error {
	if len(data) == 0 {
		return errors.New("nothing to print")
	}
	name, err := windows.UTF16PtrFromString(printer)
	if err != nil {
		return err
	}
	var h windows.Handle
	if r, _, err := procOpenPrinterW.Call(uintptr(unsafe.Pointer(name)), uintptr(unsafe.Pointer(&h)), 0); r == 0 {
		return fmt.Errorf("open printer %q: %w", printer, err)
	}
	defer procClosePrinter.Call(uintptr(h))

	docName, _ := windows.UTF16PtrFromString(doc)
	raw, _ := windows.UTF16PtrFromString("RAW")
	di := docInfo1{DocName: docName, Datatype: raw}
	if r, _, err := procStartDocPrinterW.Call(uintptr(h), 1, uintptr(unsafe.Pointer(&di))); r == 0 {
		return fmt.Errorf("start print job: %w (the driver may not accept raw data; try the Windows driver mode)", err)
	}
	defer procEndDocPrinter.Call(uintptr(h))
	if r, _, err := procStartPagePrinter.Call(uintptr(h)); r == 0 {
		return fmt.Errorf("start page: %w", err)
	}
	var written uint32
	r, _, err := procWritePrinter.Call(uintptr(h), uintptr(unsafe.Pointer(&data[0])),
		uintptr(len(data)), uintptr(unsafe.Pointer(&written)))
	procEndPagePrinter.Call(uintptr(h))
	if r == 0 {
		return fmt.Errorf("write to printer: %w", err)
	}
	if int(written) != len(data) {
		return fmt.Errorf("printer took %d of %d bytes", written, len(data))
	}
	return nil
}

// docInfoW is DOCINFOW.
type docInfoW struct {
	Size     int32
	DocName  *uint16
	Output   *uint16
	Datatype *uint16
	Type     uint32
}

// printText has the printer's driver draw lines of text in a monospaced
// font, one page; big lines at twice the size.
func printText(printer, doc string, lines []PrintLine) error {
	driver, _ := windows.UTF16PtrFromString("WINSPOOL")
	device, err := windows.UTF16PtrFromString(printer)
	if err != nil {
		return err
	}
	hdc, _, err := procCreateDCW.Call(uintptr(unsafe.Pointer(driver)), uintptr(unsafe.Pointer(device)), 0, 0)
	if hdc == 0 {
		return fmt.Errorf("open printer %q: %w", printer, err)
	}
	defer procDeleteDC.Call(hdc)

	dpi, _, _ := procGetDeviceCaps.Call(hdc, logPixelsY)
	if dpi == 0 {
		dpi = 203 // common for thermal printers
	}
	const points = 9
	face, _ := windows.UTF16PtrFromString("Consolas")
	newFont := func(scale, weight int) uintptr {
		height := -int32(int(dpi) * points * scale / 72)
		f, _, _ := procCreateFontW.Call(uintptr(height), 0, 0, 0, uintptr(weight), 0, 0, 0,
			defaultCharset, 0, 0, 0, fixedPitchModern, uintptr(unsafe.Pointer(face)))
		return f
	}
	fonts := map[[2]bool]uintptr{ // by big, bold
		{false, false}: newFont(1, 400), {false, true}: newFont(1, 700),
		{true, false}: newFont(2, 400), {true, true}: newFont(2, 700),
	}
	var old uintptr
	defer func() {
		if old != 0 {
			procSelectObject.Call(hdc, old)
		}
		for _, f := range fonts {
			if f != 0 {
				procDeleteObject.Call(f)
			}
		}
	}()

	docName, _ := windows.UTF16PtrFromString(doc)
	di := docInfoW{DocName: docName}
	di.Size = int32(unsafe.Sizeof(di))
	if r, _, err := procStartDocW.Call(hdc, uintptr(unsafe.Pointer(&di))); int32(r) <= 0 {
		return fmt.Errorf("start print job: %w", err)
	}
	if r, _, err := procStartPage.Call(hdc); int32(r) <= 0 {
		procAbortDoc.Call(hdc)
		return fmt.Errorf("start page: %w", err)
	}
	y := 0
	for _, line := range lines {
		scale := 1
		if line.Big {
			scale = 2
		}
		if f := fonts[[2]bool{line.Big, line.Bold}]; f != 0 {
			prev, _, _ := procSelectObject.Call(hdc, f)
			if old == 0 {
				old = prev
			}
		}
		if line.Text != "" {
			w, err := windows.UTF16FromString(line.Text)
			if err == nil && len(w) > 1 {
				procTextOutW.Call(hdc, 0, uintptr(y), uintptr(unsafe.Pointer(&w[0])), uintptr(len(w)-1))
			}
		}
		y += int(dpi) * points * scale * 13 / (72 * 10)
	}
	if r, _, err := procEndPage.Call(hdc); int32(r) <= 0 {
		procAbortDoc.Call(hdc)
		return fmt.Errorf("end page: %w", err)
	}
	if r, _, err := procEndDoc.Call(hdc); int32(r) <= 0 {
		return fmt.Errorf("end print job: %w", err)
	}
	return nil
}

// bitmapInfoHeader is BITMAPINFOHEADER.
type bitmapInfoHeader struct {
	Size          uint32
	Width         int32
	Height        int32
	Planes        uint16
	BitCount      uint16
	Compression   uint32
	SizeImage     uint32
	XPelsPerMeter int32
	YPelsPerMeter int32
	ClrUsed       uint32
	ClrImportant  uint32
}

// printImage has the printer's driver print a picture drawn at 203 dots an
// inch, as receipt printers print, scaled to the printer's own dots and
// kept within its printable width.
func printImage(printer, doc string, img *image.Gray) error {
	driver, _ := windows.UTF16PtrFromString("WINSPOOL")
	device, err := windows.UTF16PtrFromString(printer)
	if err != nil {
		return err
	}
	hdc, _, err := procCreateDCW.Call(uintptr(unsafe.Pointer(driver)), uintptr(unsafe.Pointer(device)), 0, 0)
	if hdc == 0 {
		return fmt.Errorf("open printer %q: %w", printer, err)
	}
	defer procDeleteDC.Call(hdc)

	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w == 0 || h == 0 {
		return errors.New("nothing to print")
	}
	// 32-bit pixels, top row first.
	pix := make([]byte, w*h*4)
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			v := img.GrayAt(b.Min.X+x, b.Min.Y+y).Y
			o := (y*w + x) * 4
			pix[o], pix[o+1], pix[o+2] = v, v, v
		}
	}
	bi := bitmapInfoHeader{Width: int32(w), Height: -int32(h), Planes: 1, BitCount: 32}
	bi.Size = uint32(unsafe.Sizeof(bi))

	dpi, _, _ := procGetDeviceCaps.Call(hdc, logPixelsX)
	if dpi == 0 {
		dpi = printerDots
	}
	dstW := w * int(dpi) / printerDots
	if room, _, _ := procGetDeviceCaps.Call(hdc, horzRes); room > 0 && dstW > int(room) {
		dstW = int(room)
	}
	dstH := h * dstW / w

	docName, _ := windows.UTF16PtrFromString(doc)
	di := docInfoW{DocName: docName}
	di.Size = int32(unsafe.Sizeof(di))
	if r, _, err := procStartDocW.Call(hdc, uintptr(unsafe.Pointer(&di))); int32(r) <= 0 {
		return fmt.Errorf("start print job: %w", err)
	}
	if r, _, err := procStartPage.Call(hdc); int32(r) <= 0 {
		procAbortDoc.Call(hdc)
		return fmt.Errorf("start page: %w", err)
	}
	r, _, err := procStretchDIBits.Call(hdc, 0, 0, uintptr(dstW), uintptr(dstH), 0, 0, uintptr(w), uintptr(h),
		uintptr(unsafe.Pointer(&pix[0])), uintptr(unsafe.Pointer(&bi)), dibRGBColors, srcCopy)
	runtime.KeepAlive(pix)
	if r == 0 {
		procAbortDoc.Call(hdc)
		return fmt.Errorf("draw the receipt: %w", err)
	}
	if r, _, err := procEndPage.Call(hdc); int32(r) <= 0 {
		procAbortDoc.Call(hdc)
		return fmt.Errorf("end page: %w", err)
	}
	if r, _, err := procEndDoc.Call(hdc); int32(r) <= 0 {
		return fmt.Errorf("end print job: %w", err)
	}
	return nil
}
