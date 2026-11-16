// Command pos is the Cafedesk point of sale: a native MyGo window for the
// shop's touch screen. Sales are saved to a local SQLite database first and
// synced to the server when it can be reached (Phase 2).
//
//	go tool mygo dev     develop, rebuilding on save
//	go test ./...        test the views without a window
//	go tool mygo build   package an installer
package main

import (
	"context"
	"log"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

func main() {
	if err := registerFonts(); err != nil {
		log.Fatal(err)
	}
	dir, err := mygo.App.Path(mygo.PathUserData)
	if err != nil {
		log.Fatal(err)
	}
	cfg, cfgErr := loadConfig(dir)
	a := newApp(cfg, dir)
	if cfgErr != nil {
		a.cfgErr = "Settings could not be read, so defaults are in use: " + cfgErr.Error()
	}

	mygo.App.WhenReady(func() {
		a.store, a.storeErr = OpenStore(context.Background(), dir)
		if a.storeErr != nil {
			log.Printf("database: %v", a.storeErr)
		}
		a.load()
		win := mygo.NewWindow(mygo.WindowOptions{
			Title:     "Cafedesk POS",
			Width:     1366,
			Height:    768,
			MinWidth:  1024,
			MinHeight: 700,
			StateKey:  "main",
			Content:   ui.View(a.view),
		})
		if cfg.FullScreen {
			win.SetFullScreen(true)
		}
		a.attach(win)
		a.loadDay() // the lock screen says whether a day is open
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
	if a.store != nil {
		a.store.Close()
	}
}
