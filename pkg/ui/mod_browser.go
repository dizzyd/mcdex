package ui

import (
	"fmt"
	"github.com/gdamore/tcell/v2"
	"github.com/rivo/tview"
	"golang.org/x/text/language"
	"golang.org/x/text/message"
	"mcdex/pkg"
	"strings"
)

type ModSelectedHandler func(slug string, loader string, mcvsn string)

type ModBrowser struct {
	app *tview.Application
	db *pkg.Database

	table *tview.Table
	loaderDropDown *tview.DropDown
	vsnDropDown *tview.DropDown

	root tview.Primitive

	focusOrder []tview.Primitive
	focusIndex int

	forgeMcVersions []string
	fabricMcVersions []string

	loader string
	mcvsn string

	onModSelected ModSelectedHandler
}

func NewModBrowser(app *tview.Application, db *pkg.Database) (*ModBrowser, error) {
	forgeMcVersions, err := db.GetSupportedMCVersions("forge")
	if err != nil {
		return nil, fmt.Errorf("failed to get supported MC version for Forge: %+v", err)
	}

	fabricMcVersions, err := db.GetSupportedMCVersions("fabric")
	if err != nil {
		return nil, fmt.Errorf("failed to get support MC versions for Fabric: %+v", err)
	}

	b := &ModBrowser{
		app: app,
		db: db,
		forgeMcVersions: forgeMcVersions,
		fabricMcVersions: fabricMcVersions,
	}

	b.table = tview.NewTable().
		SetBorders(false).
		SetFixed(1, 1).
		SetSelectable(true, false).
		SetEvaluateAllRows(true).
		SetSelectedFunc(b.modSelected).
		SetDoneFunc(b.componentDone)

	b.vsnDropDown = tview.NewDropDown().
		SetLabel("Version:").
		SetDoneFunc(b.componentDone)

	b.loaderDropDown = tview.NewDropDown().
		SetLabel("Loader:").
		SetOptions([]string{"Forge", "Fabric"}, b.loaderSelected).
		SetCurrentOption(0).
		SetDoneFunc(b.componentDone)

	b.vsnDropDown.SetBorder(true)
	b.loaderDropDown.SetBorder(true)

	b.focusOrder = []tview.Primitive{b.loaderDropDown, b.vsnDropDown, b.table}
	b.focusIndex = 0

	b.root = tview.NewFlex().
		SetDirection(tview.FlexRow).
		AddItem(tview.NewFlex().
			AddItem(b.loaderDropDown, 0, 1, true).
			AddItem(b.vsnDropDown, 0, 1, true),
			0, 1, true).
		AddItem(b.table, 0, 10, true)

	return b, nil
}

func (b *ModBrowser) SetModSelectedFunc(f ModSelectedHandler) {
	b.onModSelected = f
}

func (b *ModBrowser) RootView() tview.Primitive {
	return b.root
}

func (b *ModBrowser) loaderSelected(name string, index int) {
	b.loader = strings.ToLower(name)
	b.refreshVersions()
}

func (b *ModBrowser) versionSelected(name string, index int) {
	b.mcvsn = name
	b.refreshTable()
}

func (b *ModBrowser) modSelected(row, column int) {
	slug := b.table.GetCell(row, 0).Text
	if b.onModSelected != nil {
		b.onModSelected(slug, b.loader, b.mcvsn)
	}
}

func (b *ModBrowser) componentDone(key tcell.Key) {
	if key == tcell.KeyTab {
		b.focusIndex = (b.focusIndex+1) % len(b.focusOrder)
		b.app.SetFocus(b.focusOrder[b.focusIndex])
	}
}

func (b *ModBrowser) refreshVersions() {
	if b.loader == "forge" {
		b.vsnDropDown.SetOptions(b.forgeMcVersions, b.versionSelected)
	} else {
		b.vsnDropDown.SetOptions(b.fabricMcVersions, b.versionSelected)
	}
	b.vsnDropDown.SetCurrentOption(0)
}

func (b *ModBrowser) refreshTable() {
	row := 1
	printer := message.NewPrinter(language.English)

	b.table.Clear()

	b.table.SetCell(0, 0, tview.NewTableCell("Slug").SetSelectable(false))
	b.table.SetCell(0, 1, tview.NewTableCell("Downloads").SetSelectable(false))
	b.table.SetCell(0, 2, tview.NewTableCell("Loader").SetSelectable(false))
	b.table.SetCell(0, 3, tview.NewTableCell("Desc").SetSelectable(false))

	b.db.ForEachMod(b.mcvsn, b.loader, func(id int, slug string, loader string, description string, downloads int) error {
		b.table.SetCell(row, 0, tview.NewTableCell(slug).SetMaxWidth(25))
		b.table.SetCell(row, 1, tview.NewTableCell(printer.Sprintf("%d", downloads)))
		b.table.SetCell(row, 2, tview.NewTableCell(loader))
		b.table.SetCell(row, 3, tview.NewTableCell(description).SetMaxWidth(150))
		row++
		return nil
	})
}