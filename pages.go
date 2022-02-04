package main

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"code.rocketnine.space/tslocum/cview"
	"github.com/diamondburned/arikawa/v3/api"
	"github.com/diamondburned/arikawa/v3/discord"
	"github.com/gdamore/tcell/v2"
	"github.com/zalando/go-keyring"
)

type mainPage struct {
	*cview.Flex
	tabs struct {
		*cview.TabbedPanels
		login  *loginPage
		guilds *guildView
		delete *deletePage
	}
	console *consoleView
}

func newMainPage(ctx context.Context) *mainPage {
	m := mainPage{}

	m.console = newConsoleView()
	ctx = m.console.WithLogger(ctx)

	m.tabs.login = newLoginPage(ctx)
	m.tabs.login.SetNextFunc(func() {
		m.tabs.SetCurrentTab("guilds")
		m.tabs.guilds.Reload()
	})
	ctx = m.tabs.login.WithClient(ctx)

	state := deleteState{}
	m.tabs.guilds = newGuildView(ctx, &state)
	m.tabs.delete = newDeletePage(ctx, &state)

	// tview fucking sucks. There's no way to get the internal Panels, so
	// SetBackgroundTransparent doesn't work properly by itself. How wonderful.
	// After many years, the API is still fully of unexported fields that make
	// using the primitives a miserable experience.
	m.tabs.TabbedPanels = cview.NewTabbedPanels()
	m.tabs.AddTab("login", "Login", m.tabs.login)
	m.tabs.AddTab("guilds", "Guilds", m.tabs.guilds)
	m.tabs.AddTab("delete", "Delete", m.tabs.delete)

	m.Flex = cview.NewFlex()
	m.Flex.SetDirection(cview.FlexRow)
	m.Flex.AddItem(m.tabs, 0, 2, true)
	m.Flex.AddItem(m.console, 0, 1, true)

	return &m
}

type consoleView struct {
	*cview.Frame
	text   *cview.TextView
	logger *log.Logger
}

func newConsoleView() *consoleView {
	c := consoleView{}
	c.text = cview.NewTextView()
	c.text.SetDynamicColors(false)
	c.text.ScrollToEnd()
	c.text.SetText("Begin console.\n")

	c.logger = log.New(c.text, "", log.LstdFlags)

	c.Frame = cview.NewFrame(c.text)
	c.Frame.SetBorder(true)
	c.Frame.SetBorderAttributes(tcell.AttrDim)

	return &c
}

func (c *consoleView) WithLogger(ctx context.Context) context.Context {
	return context.WithValue(ctx, loggerKey, c.logger)
}

type loginPage struct {
	cview.Primitive
	token  *cview.InputField
	next   *cview.Button
	client *api.Client

	ctx context.Context
}

const keyringID = "github.com/diamondburned/wipe-discord"

func newLoginPage(ctx context.Context) *loginPage {
	l := loginPage{ctx: ctx}
	l.client = api.NewClient("")
	l.client.UserAgent = "Mozilla/5.0 (X11; Linux x86_64; rv:96.0) Gecko/20100101 Firefox/96.0"

	l.token = cview.NewInputField()
	l.token.SetLabel("Token ")
	l.token.SetMaskCharacter('*')
	l.token.SetChangedFunc(func(text string) { l.client.Token = text })

	go func() {
		token, err := keyring.Get(keyringID, "token")
		if err != nil {
			return
		}

		app := appFromContext(ctx)
		app.IdleAdd(func() { l.token.SetText(token) })
	}()

	text := cview.NewTextView()
	text.SetText(`Input your token below.

Instructions for finding the account token can be found at https://discordhelp.net/discord-token.`)

	remember := cview.NewButton("Remember (encrypted)")
	remember.SetBackgroundColor(tcell.ColorGray)
	remember.SetSelectedFunc(l.RememberMe)

	l.next = cview.NewButton("Next")

	buttons := cview.NewFlex()
	buttons.AddItem(remember, 0, 1, true)
	buttons.AddItem(nil, 3, 0, false)
	buttons.AddItem(l.next, 0, 1, true)

	flex := cview.NewFlex()
	flex.SetDirection(cview.FlexRow)
	flex.AddItem(nil, 0, 2, false)
	flex.AddItem(text, 4, 0, false)
	flex.AddItem(l.token, 3, 0, true)
	flex.AddItem(centerPrimitive(buttons, 75, 0), 3, 0, true)
	flex.AddItem(nil, 0, 1, false)

	l.Primitive = centerPrimitive(flex, 100, 0)

	return &l
}

func (l *loginPage) RememberMe() {
	log := loggerFromContext(l.ctx)

	if err := keyring.Set(keyringID, "token", l.client.Token); err != nil {
		log.Println("cannot remember token:", err)
	} else {
		log.Println("Token saved into the system keyring. " +
			"It'll be used automatically when wipe-discord is started again.")
	}
}

func (l *loginPage) SetNextFunc(f func()) {
	l.next.SetSelectedFunc(f)
	l.token.SetDoneFunc(func(key tcell.Key) {
		if key == tcell.KeyEnter {
			f()
		}
	})
}

func (l *loginPage) WithClient(ctx context.Context) context.Context {
	return context.WithValue(ctx, clientKey, l.client)
}

type deleteState struct {
	selected map[discord.GuildID]bool
	guilds   map[discord.GuildID]discord.Guild
	selfID   discord.UserID
}

func (s *deleteState) SelectedIDs() []discord.GuildID {
	ids := make([]discord.GuildID, 0, len(s.selected))
	for id := range s.selected {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool {
		return ids[i] < ids[j]
	})
	return ids
}

type guildView struct {
	*cview.Flex
	loading *cview.TextView
	list    *guildList

	ctx   context.Context
	state *deleteState
}

type guildList struct {
	centerBox
	list  *cview.List
	items []*cview.ListItem
}

func newGuildView(ctx context.Context, state *deleteState) *guildView {
	l := guildView{
		ctx:   ctx,
		state: state,
	}

	l.loading = cview.NewTextView()
	l.loading.SetTextAlign(cview.AlignCenter)
	l.loading.SetVerticalAlign(cview.AlignMiddle)

	l.list = &guildList{}

	l.list.list = cview.NewList()
	l.list.list.SetWrapAround(true)
	l.list.list.ShowSecondaryText(false)
	l.list.list.SetSelectedFunc(func(_ int, item *cview.ListItem) {
		guildID := item.GetReference().(discord.GuildID)
		guild := l.state.guilds[guildID]

		_, selected := l.state.selected[guildID]
		if selected {
			delete(l.state.selected, guildID)
			item.SetMainText(guild.Name)
		} else {
			l.state.selected[guildID] = true
			item.SetMainText(guild.Name + " (*)")
		}
	})

	l.list.centerBox = centerPrimitive(l.list.list, 100, 0)

	reload := cview.NewButton("Reload")
	reload.SetSelectedFunc(l.Reload)
	reload.SetBorder(false)

	search := cview.NewInputField()
	search.SetPlaceholder("Search guilds...")
	search.SetChangedFunc(l.Search)

	l.Flex = cview.NewFlex()
	l.Flex.SetDirection(cview.FlexRow)
	l.Flex.AddItem(centerPrimitive(reload, 10, 0), 1, 0, true)
	l.Flex.AddItem(centerPrimitive(search, 80, 1), 3, 0, true)
	l.Flex.AddItem(l.list, 0, 1, true)

	return &l
}

func (l *guildView) Reload() {
	l.RemoveItem(l.list)
	l.RemoveItem(l.loading)

	*l.list = guildList{
		centerBox: l.list.centerBox,
		list:      l.list.list,
	}
	l.list.list.Clear()

	l.AddItem(l.loading, 0, 1, true)
	l.loading.SetText("Loading Guilds...")

	go func() {
		app := appFromContext(l.ctx)
		api := apiFromContext(l.ctx)

		me, err := api.Me()
		if err != nil {
			app.IdleAdd(func() { l.loading.SetText("Error: " + err.Error()) })
			return
		}

		guilds, err := api.Guilds(0)
		if err != nil {
			app.IdleAdd(func() { l.loading.SetText("Error: " + err.Error()) })
			return
		}

		guildMap := make(map[discord.GuildID]discord.Guild, len(guilds))
		for _, guild := range guilds {
			guildMap[guild.ID] = guild
		}

		app.IdleAdd(func() {
			l.state.selfID = me.ID
			l.state.guilds = guildMap
			l.state.selected = make(map[discord.GuildID]bool)

			l.list.items = make([]*cview.ListItem, len(l.state.guilds))

			for i, guild := range guilds {
				item := cview.NewListItem(guild.Name)
				item.SetReference(guild.ID)

				l.list.list.AddItem(item)
				l.list.items[i] = item
			}

			l.RemoveItem(l.loading)
			l.AddItem(l.list, 0, 1, true)
		})
	}()
}

func (l *guildView) Search(search string) {
	l.list.list.Clear()

	for _, item := range l.list.items {
		guildID := item.GetReference().(discord.GuildID)

		guild := l.state.guilds[guildID]
		if search == "" || containsFold(guild.Name, search) {
			l.list.list.AddItem(item)
		}
	}
}

func containsFold(i, j string) bool {
	return strings.Contains(strings.ToLower(i), strings.ToLower(j))
}

/*
type errorModal struct {
	*cview.Modal
}

func newErrorModal(err error, prefix string, showOK bool) *errorModal {
	report := cview.NewModal()
	report.SetText(prefix + ":\n" + err.Error())

	if showOK {
		report.AddButtons([]string{"OK"})
	}

	return &errorModal{report}
}

func (m *errorModal) SetOKFunc(f func()) {
	m.SetDoneFunc(func(ix int, label string) {
		f()
	})
}
*/

type deletePage struct {
	cview.Primitive
	from    *cview.InputField
	to      *cview.InputField
	query   *cview.InputField
	preview *cview.TextView

	ctx   context.Context
	state struct {
		*deleteState
		from  time.Time
		to    time.Time
		query string
	}
}

const dateFmt = "2006/01/02 15:04" // YYYY/MM/DD HH:MM

func parseInputTime(input *cview.InputField, dst *time.Time) func(string) {
	return func(text string) {
		if text == "" {
			*dst = time.Time{}
			input.ResetFieldNote()
			return
		}

		t, err := time.Parse(dateFmt, text)
		if err != nil {
			input.SetFieldNote(err.Error())
		} else {
			input.ResetFieldNote()
			*dst = t
		}
	}
}

func newDeletePage(ctx context.Context, state *deleteState) *deletePage {
	d := deletePage{ctx: ctx}
	d.state.deleteState = state

	d.from = cview.NewInputField()
	d.from.SetLabel(pad(15, "From Date"))
	d.from.SetPlaceholder("YYYY/MM/DD HH:MM (optional)")
	d.from.SetFieldNoteTextColor(tcell.ColorRed)
	d.from.SetChangedFunc(parseInputTime(d.from, &d.state.from))

	d.to = cview.NewInputField()
	d.to.SetLabel(pad(15, "To Date"))
	d.to.SetPlaceholder("YYYY/MM/DD HH:MM (optional)")
	d.to.SetFieldNoteTextColor(tcell.ColorRed)
	d.to.SetChangedFunc(parseInputTime(d.to, &d.state.to))

	d.query = cview.NewInputField()
	d.query.SetLabel(pad(15, "Search Query"))
	d.query.SetChangedFunc(func(text string) { d.state.query = text })

	calculate := cview.NewButton("Calculate")
	calculate.SetSelectedFunc(d.calculate)

	delete := cview.NewButton("Delete")
	delete.SetBackgroundColor(tcell.ColorRed)

	cancel := cview.NewButton("Cancel")
	cancel.SetBackgroundColor(tcell.ColorBlue)

	buttons := cview.NewFlex()
	buttons.SetDirection(cview.FlexColumn)
	buttons.AddItem(nil, 0, 1, false)
	buttons.AddItem(calculate, 0, 1, true)
	buttons.AddItem(nil, 3, 0, false)
	buttons.AddItem(delete, 0, 1, true)
	buttons.AddItem(nil, 3, 0, false)
	buttons.AddItem(cancel, 0, 1, true)
	buttons.AddItem(nil, 0, 1, false)

	flex := cview.NewFlex()
	flex.SetDirection(cview.FlexRow)
	flex.AddItem(nil, 0, 1, false)
	flex.AddItem(d.from, 3, 0, true)
	flex.AddItem(d.to, 3, 0, true)
	flex.AddItem(d.query, 3, 0, true)
	flex.AddItem(nil, 0, 1, false)
	flex.AddItem(buttons, 3, 0, true)

	d.Primitive = centerPrimitive(flex, 99, 0)

	return &d
}

func pad(len int, str string) string {
	return fmt.Sprintf("%[1]*[2]s ", len-1, str)
}

func (d *deletePage) calculate() {
	log := loggerFromContext(d.ctx)
	client := apiFromContext(d.ctx)
	guildIDs := d.state.SelectedIDs()

	var count uint

	for _, guildID := range guildIDs {
		guild := d.state.guilds[guildID]
		log.Printf("Checking guild %q", guild.Name)

		data := api.SearchData{
			Content:  d.state.query,
			AuthorID: d.state.selfID,
		}
		if !d.state.from.IsZero() {
			data.MinID = discord.MessageID(discord.NewSnowflake(d.state.from))
		}
		if !d.state.to.IsZero() {
			data.MaxID = discord.MessageID(discord.NewSnowflake(d.state.to))
		}

		r, err := client.Search(guildID, data)
		if err != nil {
			log.Println("    error:", err)
		} else {
			log.Println("    found", r.TotalResults, "messages")
			count += r.TotalResults
		}
	}

	log.Println("Total:", count, "messages to be deleted.")
}

func (d *deletePage) delete() {}
