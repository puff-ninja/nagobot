package stt

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// Transcriber converts audio data to text.
type Transcriber interface {
	Transcribe(ctx context.Context, audioData []byte) (string, error)
}

// GoogleSTT implements Transcriber using the Google Cloud Speech-to-Text v1 REST API.
type GoogleSTT struct {
	apiKey       string
	languageCode string
	client       *http.Client
}

// NewGoogleSTT creates a new Google STT transcriber.
func NewGoogleSTT(apiKey, languageCode string) *GoogleSTT {
	if languageCode == "" {
		languageCode = "zh-CN"
	}
	return &GoogleSTT{
		apiKey:       apiKey,
		languageCode: languageCode,
		client:       &http.Client{},
	}
}

type sttRequest struct {
	Config sttConfig `json:"config"`
	Audio  sttAudio  `json:"audio"`
}

type sttConfig struct {
	Encoding        string `json:"encoding"`
	SampleRateHertz int    `json:"sampleRateHertz"`
	LanguageCode    string `json:"languageCode"`
}

type sttAudio struct {
	Content string `json:"content"`
}

type sttResponse struct {
	Results []sttResult `json:"results"`
	Error   *sttError   `json:"error,omitempty"`
}

type sttResult struct {
	Alternatives []sttAlternative `json:"alternatives"`
}

type sttAlternative struct {
	Transcript string  `json:"transcript"`
	Confidence float64 `json:"confidence"`
}

type sttError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Transcribe sends audio to Google Cloud Speech-to-Text and returns the transcript.
// Audio is expected to be OGG/Opus format (Discord voice messages).
func (g *GoogleSTT) Transcribe(ctx context.Context, audioData []byte) (string, error) {
	reqBody := sttRequest{
		Config: sttConfig{
			Encoding:        "OGG_OPUS",
			SampleRateHertz: 48000,
			LanguageCode:    g.languageCode,
		},
		Audio: sttAudio{
			Content: base64.StdEncoding.EncodeToString(audioData),
		},
	}

	payload, err := json.Marshal(reqBody)
	if err != nil {
		return "", fmt.Errorf("marshal stt request: %w", err)
	}

	url := "https://speech.googleapis.com/v1/speech:recognize?key=" + g.apiKey
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(payload))
	if err != nil {
		return "", fmt.Errorf("create stt request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := g.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("stt request: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read stt response: %w", err)
	}

	var sttResp sttResponse
	if err := json.Unmarshal(body, &sttResp); err != nil {
		return "", fmt.Errorf("parse stt response: %w", err)
	}

	if sttResp.Error != nil {
		return "", fmt.Errorf("google stt error %d: %s", sttResp.Error.Code, sttResp.Error.Message)
	}

	var parts []string
	for _, r := range sttResp.Results {
		if len(r.Alternatives) > 0 {
			parts = append(parts, r.Alternatives[0].Transcript)
		}
	}

	if len(parts) == 0 {
		return "", fmt.Errorf("no transcription results")
	}

	return strings.Join(parts, " "), nil
}
