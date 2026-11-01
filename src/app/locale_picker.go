package app

// locale_picker.go — the app-level language section of Settings → General.
//
// The catalogs (locales/en.json, locales/zh-Hans.json) are compiled by
// `mygo generate` into gocode/locales, whose import registers their
// languages with package i18n. The language picked here is the one the whole
// window shows: our own messages, MyGo's controls and dialogs (its editor,
// diff viewer and Markdown view say their own words in it), and the numbers
// and dates CLDR writes for the region. It is kept for the next launches.

import (
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/i18n"
	"github.com/egoist/mygo/ui"

	"gocode/locales"
)

// cat returns the catalogs' messages in the window's language. mygo keeps the
// engine's language (c.Locale) in step with the app's, so saying a message
// through the window says it in the language the reader sees — and a headless
// Tester drives the whole UI with tt.SetLocale, which no global lookup would
// follow. Off the view (goroutines, saved data), the package-level functions
// say the same messages in the app's language.
func cat(c *ui.Context) locales.Messages { return locales.In(c.Locale()) }

// viewLanguageSection renders the language picker: every language the app is
// written in, each named in itself, with the current one pressed and a way
// back to following the system's languages.
//
// The current language is read from the context (c.Locale), not mygo.App:
// App.Locale works the language out on the main thread the first time, which
// in a headless test — no event loop — never returns.
func (s *Shell) viewLanguageSection(c *ui.Context) {
	t := c.Theme()
	ui.Text(c, cat(c).LanguageSection()).FontSize(14).Bold()
	ui.Text(c, cat(c).LanguageHelp()).FontSize(12).TextColor(t.TextMuted).Wrap()
	current := c.Locale()
	ui.Row(c.Key("lang-row")).Gap(6).Children(func() {
		for _, tag := range mygo.App.Languages() {
			label := i18n.Autonym(tag) // "English", "简体中文"
			tag := tag
			var clicked bool
			if tag == current {
				clicked = ui.PrimaryButton(c.Key("lang-"+tag), label).Clicked()
			} else {
				clicked = ui.Button(c.Key("lang-"+tag), label).Clicked()
			}
			if clicked {
				mygo.App.SetLocale(tag)
			}
		}
		if current != "" {
			if ui.Button(c.Key("lang-system"), cat(c).LanguageFollowSystem()).Clicked() {
				mygo.App.SetLocale("") // follow the system's languages again
			}
		}
	})
	_ = t
}
