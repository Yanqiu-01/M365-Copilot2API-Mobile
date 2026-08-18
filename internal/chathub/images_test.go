package chathub

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestImageURLs(t *testing.T) {
	raw := []json.RawMessage{
		json.RawMessage(`{"content":{"image":{"downloadUrl":"https://cdn.example.com/image/1.png","thumbnailUrl":"https://cdn.example.com/image/1.png"}},"url":"https://example.com/page"}`),
		json.RawMessage(`{"src":"https://cdn.example.com/image/2.webp"}`),
	}
	got := imageURLs(raw)
	if len(got) != 2 {
		t.Fatalf("got %v", got)
	}
}

func TestImageURLsImageReferenceUrls(t *testing.T) {
	// Captured live from ChatHub image generation progress message
	// (contentGenerationProgressList[0].ImageReferenceUrls).
	raw := []json.RawMessage{json.RawMessage(`{
	  "type": 1,
	  "target": "update",
	  "arguments": [{
	    "messages": [{
	      "text": "Loading image",
	      "contentGenerationProgressList": [{
	        "contentType": "image",
	        "pollUrl": "eyJQb2xsSWQiOiJjZTRjMjYyZiIsIkZpbGVUb2tlbiI6InRva19oZXJlIn0=",
	        "fileToken": "e4dbaf90-17b5-4025-a4a8-5ff377c7849c",
	        "ImageReferenceUrls": [
	          "https://designerapp.officeapps.live.com/designerapp/document.ashx?path=%2Fe0b90f31-23a9-41cb-b556-8eb15d31b16c%2FDallEGeneratedImages%2Fdalle-a7176d65-397f-475f-a470-81da39f348cd0251616132911800150700.png&dcHint=KoreaCentral&speCId=a899c570-5d1e-49e7-b272-9a5ffc47427c&speType=Image&speIdx=0&fileToken=eyJUb2tlblByZWZpeCI6IkFBRC03In0"
	        ]
	      }],
	      "contentType": "GraphicArt",
	      "contentOrigin": "ImageGeneration"
	    }]
	  }]
	}`)}
	got := imageURLs(raw)
	if len(got) != 1 {
		t.Fatalf("got %v", got)
	}
	if !strings.Contains(got[0], "designerapp.officeapps.live.com") {
		t.Fatalf("unexpected url %q", got[0])
	}
}

func TestImageURLsRejectsUnsafe(t *testing.T) {
	raw := []json.RawMessage{json.RawMessage(`{"url":"http://example.com/a.png"}`)}
	if got := imageURLs(raw); len(got) != 0 {
		t.Fatal(got)
	}
}
