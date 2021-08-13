package ui

import (
	"fmt"
	"github.com/pkg/browser"
	"github.com/rivo/tview"
	"mcdex/pkg"
)

type Explorer struct {
	app *tview.Application
	db *pkg.Database

	modBrowser *ModBrowser

	pages *tview.Pages
}

func NewExplorer(db *pkg.Database) (*Explorer, error) {
	var err error
	e := &Explorer{db: db, app: tview.NewApplication()}
	e.app.EnableMouse(false)

	e.modBrowser, err = NewModBrowser(e.app, db)
	if err != nil {
		return nil, fmt.Errorf("error initializing mod browser: %+v", err)
	}
	e.modBrowser.SetModSelectedFunc(e.showModDetail)

	e.pages = tview.NewPages().
		AddPage("mod_browser", e.modBrowser.RootView(), true, true)

	e.app.SetRoot(e.pages, true)

	return e, nil
}

func (e *Explorer) Run() error {
	return e.app.Run()
}

func (e *Explorer) showModDetail(slug string, loader string, mcvsn string) {
	url := fmt.Sprintf("https://www.curseforge.com/minecraft/mc-mods/%s", slug)
	browser.OpenURL(url)
}

func makeCenteredModal(p tview.Primitive, width, height int) tview.Primitive {
	// Generate a new flex that has 3 columns, with center column "width" wide; within the centered
	// column, generate ANOTHER new flex by rows that has the center row "height" tall
	return tview.NewFlex().
		AddItem(nil, 0, 1, false).
		AddItem(tview.NewFlex().SetDirection(tview.FlexRow).
			AddItem(nil, 0, 1, false).
			AddItem(p, height, 1, false).
			AddItem(nil, 0, 1, false), width, 1, false).
		AddItem(nil, 0, 1, false)
}