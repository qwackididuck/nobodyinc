package main

import (
	"context"
	"strings"
	"time"

	"github.com/qwackididuck/nobodyinc/internal/updates"
)

// releasePrefix is where a release page lives: the menu opens nothing else.
const releasePrefix = "https://github.com/"

// updateCheckTimeout bounds the check made from the menu.
const updateCheckTimeout = 10 * time.Second

// checkForUpdates is File > Check for Updates…: it asks the launcher to look
// for a newer release and tells the human what it found. The control room's
// banner shows the same result (docs/updates.md). Run it off the main
// thread: it waits for GitHub.
func (d *desktop) checkForUpdates() {
	ctx, cancel := context.WithTimeout(context.Background(), updateCheckTimeout)
	defer cancel()

	status := d.launch.CheckUpdate(ctx)

	switch {
	case status.Error != "":
		d.app.Dialog.Error().SetTitle("Could not check for updates").
			SetMessage(status.Error).Show()
	case status.Available:
		dialog := d.app.Dialog.Info().SetTitle("A new version is available").
			SetMessage("Nobody Inc " + status.Latest + " is out; this is " + status.Current + ".")

		dialog.AddButton("Open the release page").OnClick(func() {
			// The URL comes from GitHub's answer: open only a GitHub page.
			if !strings.HasPrefix(status.URL, releasePrefix) {
				return
			}

			if err := d.app.Browser.OpenURL(status.URL); err != nil {
				d.logger.Warn("could not open the release page", "err", err)
			}
		})
		dialog.AddButton("Later").SetAsCancel()
		dialog.Show()
	case updates.IsRelease(status.Current):
		d.app.Dialog.Info().SetTitle("Nobody Inc is up to date").
			SetMessage("You have " + status.Current + ", the latest release.").Show()
	default:
		d.app.Dialog.Info().SetTitle("Development build").
			SetMessage("This build (" + status.Current + ") is not a release: it is never compared with the latest one.").Show()
	}
}
