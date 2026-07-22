package main

type SuggestionResponse struct {
	Suggestions []Suggestion `json:"Suggestions"`
}

type Suggestion struct {
	Attributes SuggestionAttributes `json:"Attributes"`
	Text       string               `json:"Text"`
}

type SuggestionAttributes struct {
	URL            string `json:"url"`
	Query          string `json:"query"`
	PreviewPaneURL string `json:"previewPaneUrl,omitempty"`
	PreviewURL     string `json:"previewUrl,omitempty"`
}

type PreviewData struct {
	FileName    string
	FilePath    string
	FileSize    string
	FileDate    string
	FileExt     string
	Icon        string
	FileURL     string
	DirURL      string
	DirPath     string
	IsFolder    bool
	IsImage     bool
	IsVideo     bool
	IsAudio     bool
	IsText      bool
	ImageSrcURL string
	MediaSrcURL string
	TextContent string

	HasMediaInfo    bool
	MediaDuration   string
	MediaResolution string
	MediaCodec      string

	StrPreviewTitle string
	StrPath         string
	StrSize         string
	StrModified     string
	StrOpenFile     string
	StrOpenLocation string
	StrResolution   string
	StrDuration     string
	StrCodec        string
}

type FFprobeOutput struct {
	Streams []struct {
		CodecType string `json:"codec_type"`
		CodecName string `json:"codec_name"`
		Width     int    `json:"width"`
		Height    int    `json:"height"`
	} `json:"streams"`
	Format struct {
		Duration string `json:"duration"`
	} `json:"format"`
}

type detailedResult struct {
	name string
	path string
	size string
	date string
}
