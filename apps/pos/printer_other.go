//go:build !windows

package main

import (
	"errors"
	"image"
)

// Printing through a spooler is only written for Windows so far. macOS and
// Linux would print through CUPS.

var errNoSpooler = errors.New("printing is only supported on Windows for now")

func listPrinters() ([]string, error)                        { return nil, nil }
func defaultPrinter() (string, error)                        { return "", errNoSpooler }
func printRaw(printer, doc string, data []byte) error        { return errNoSpooler }
func printText(printer, doc string, lines []PrintLine) error { return errNoSpooler }
func printImage(printer, doc string, img *image.Gray) error  { return errNoSpooler }
