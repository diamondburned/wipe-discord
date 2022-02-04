package main

import (
	"context"
	"log"
	"os"
	"os/signal"

	"code.rocketnine.space/tslocum/cview"
	"github.com/diamondburned/arikawa/v3/api"
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	app := newApplication()
	app.EnableMouse(true)

	ctx = context.WithValue(ctx, applicationKey, app)

	main := newMainPage(ctx)
	app.SetRoot(main)

	go func() {
		<-ctx.Done()
		app.QueueUpdate(func() { app.Stop() })
	}()

	if err := app.Run(); err != nil {
		log.Fatalln("cannot run:", err)
	}
}

type contextKey uint8

const (
	_ contextKey = iota
	applicationKey
	clientKey
	loggerKey
)

func appFromContext(ctx context.Context) *application {
	app := ctx.Value(applicationKey).(*application)
	return app
}

func apiFromContext(ctx context.Context) *api.Client {
	client := ctx.Value(clientKey).(*api.Client)
	return client
}

func loggerFromContext(ctx context.Context) *log.Logger {
	logger := ctx.Value(loggerKey).(*log.Logger)
	if logger == nil {
		return log.Default()
	}
	return logger
}

type application struct {
	*cview.Application
	root cview.Primitive
}

func newApplication() *application {
	return &application{
		Application: cview.NewApplication(),
	}
}

func (a *application) SetRoot(root cview.Primitive) {
	a.root = root
	a.Application.SetRoot(root, true)
}

func (a *application) Root() cview.Primitive {
	return a.root
}

// ShowModal shows the modal in front of the application until any of the
// buttons are clickedi. f is then called.
func (a *application) ShowModal(modal *cview.Modal, f func(ix int)) {
	root := a.root
	a.Application.SetRoot(modal, false)

	modal.SetDoneFunc(func(ix int, _ string) {
		a.Application.SetRoot(root, true)
		f(ix)
	})
}

func (a *application) IdleAdd(f func()) {
	a.Application.QueueUpdateDraw(f)
}
