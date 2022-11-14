package messages

import (
	"bytes"
	"embed"
	"github.com/slack-go/slack"
	"html/template"
	"io/ioutil"
	"log"

	"encoding/json"
)

func getHomeView() slack.HomeTabViewRequest {
	var appHomeAssets embed.FS
	// Base elements
	str, err := appHomeAssets.ReadFile("appHomeViewsAssets/AppHomeView.json")
	if err != nil {
		log.Printf("Unable to read view `AppHomeView`: %v", err)
	}
	view := slack.HomeTabViewRequest{}
	json.Unmarshal(str, &view)

	// New Notes
	t, err := template.ParseFS(appHomeAssets, "appHomeViewsAssets/NoteBlock.json")
	if err != nil {
		panic(err)
	}
	var tpl bytes.Buffer
	note := ""
	err = t.Execute(&tpl, note)
	if err != nil {
		panic(err)
	}
	str, _ = ioutil.ReadAll(&tpl)
	note_view := slack.HomeTabViewRequest{}
	json.Unmarshal(str, &note_view)

	view.Blocks.BlockSet = append(view.Blocks.BlockSet, note_view.Blocks.BlockSet...)

	return view
}
