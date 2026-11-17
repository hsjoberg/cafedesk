package main

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

// Config is the terminal's local settings, kept as config.json in the app's
// user data directory. Phase 2 replaces most of it with what pairing and
// the server send down.
type Config struct {
	ShopName     string `json:"shopName"`
	TerminalCode string `json:"terminalCode"` // prefix of order numbers, e.g. "T1"

	// Placeholders until the terminal is paired with the server.
	OrgID      string `json:"orgId"`
	ShopID     string `json:"shopId"`
	TerminalID string `json:"terminalId"`

	ServerURL string `json:"serverUrl"`
	ShopLabel string `json:"shopLabel"` // the shop's name in the admin, once paired

	ReceiptFooter string `json:"receiptFooter"` // the last line of a receipt; "Thank you" when empty

	ReceiptPrinter   string `json:"receiptPrinter"`   // Windows printer name
	PrintMode        string `json:"printMode"`        // "escpos" (raw bytes) or "windows" (the driver draws text)
	ReceiptWidth     int    `json:"receiptWidth"`     // characters per line: 48 for 80 mm paper, 32 for 58 mm
	OpenDrawer       bool   `json:"openDrawer"`       // pulse a drawer on the receipt printer for cash sales
	KitchenEnabled   bool   `json:"kitchenEnabled"`   // print kitchen tickets
	KitchenPrinter   string `json:"kitchenPrinter"`   // Windows printer name
	KitchenPrintMode string `json:"kitchenPrintMode"` // as PrintMode

	BackupDir string `json:"backupDir"` // where backups go; defaultBackupDir() when empty

	FullScreen bool `json:"fullScreen"`

	// DayEnds is when a day still open closes by itself, without a count:
	// "04:00", or "off". Empty is the default, defaultDayEnds.
	DayEnds string `json:"dayEnds"`
}

const (
	defaultDayEnds = "04:00"
	dayEndsOff     = "off"
)

// dayEnds is when a day still open closes by itself, or dayEndsOff.
func (c Config) dayEnds() string {
	if c.DayEnds == "" {
		return defaultDayEnds
	}
	return c.DayEnds
}

// Printer roles.
const (
	roleReceipt = "receipt"
	roleKitchen = "kitchen"
)

// printer returns the printer and mode of a role.
func (c Config) printer(role string) (name, mode string) {
	if role == roleKitchen {
		return c.KitchenPrinter, c.KitchenPrintMode
	}
	return c.ReceiptPrinter, c.PrintMode
}

// backupDir is where backups go.
func (c Config) backupDir() string {
	if c.BackupDir != "" {
		return c.BackupDir
	}
	return defaultBackupDir()
}

// Print modes.
const (
	printESCPOS  = "escpos"
	printWindows = "windows"
)

func defaultConfig() Config {
	return Config{
		ShopName:         "Cafedesk",
		TerminalCode:     "T1",
		OrgID:            "local",
		ShopID:           "local",
		TerminalID:       "T1",
		ServerURL:        "http://localhost:8080",
		PrintMode:        printESCPOS,
		KitchenPrintMode: printESCPOS,
		ReceiptWidth:     48,
	}
}

// loadConfig reads config.json from dir, writing the defaults there first
// if it does not exist. CAFEDESK_SERVER_URL overrides the server URL.
func loadConfig(dir string) (Config, error) {
	cfg := defaultConfig()
	data, err := os.ReadFile(filepath.Join(dir, "config.json"))
	switch {
	case errors.Is(err, fs.ErrNotExist):
		if err := saveConfig(dir, cfg); err != nil {
			return cfg, err
		}
	case err != nil:
		return cfg, err
	default:
		if err := json.Unmarshal(data, &cfg); err != nil {
			return defaultConfig(), err
		}
	}
	if u := os.Getenv("CAFEDESK_SERVER_URL"); u != "" {
		cfg.ServerURL = u
	}
	if cfg.ReceiptWidth < 24 {
		cfg.ReceiptWidth = 48
	}
	if cfg.PrintMode != printWindows {
		cfg.PrintMode = printESCPOS
	}
	if cfg.KitchenPrintMode != printWindows {
		cfg.KitchenPrintMode = printESCPOS
	}
	return cfg, nil
}

// saveConfig writes cfg to dir/config.json, replacing it in one step.
func saveConfig(dir string, cfg Config) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	tmp := filepath.Join(dir, "config.json.tmp")
	if err := os.WriteFile(tmp, append(data, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(dir, "config.json"))
}
