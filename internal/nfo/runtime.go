package nfo

import (
	"encoding/xml"
	"fmt"
	"strconv"
	"strings"
)

// Some scrapers put a minute suffix in runtime/duration. Decode those fields
// separately while keeping numeric fields and generated NFOs unchanged.
func (m *MovieMeta) UnmarshalXML(decoder *xml.Decoder, start xml.StartElement) error {
	type plain MovieMeta
	value := struct {
		*plain
		Runtime string `xml:"runtime"`
	}{plain: (*plain)(m)}
	if err := decoder.DecodeElement(&value, &start); err != nil {
		return err
	}
	minutes, err := parseMinuteValue(value.Runtime, 64)
	if err != nil {
		return fmt.Errorf("nfo: runtime=%q: %w", value.Runtime, err)
	}
	m.Runtime = minutes
	return nil
}

func (video *VideoStream) UnmarshalXML(decoder *xml.Decoder, start xml.StartElement) error {
	type plain VideoStream
	value := struct {
		*plain
		Duration string `xml:"duration"`
	}{plain: (*plain)(video)}
	if err := decoder.DecodeElement(&value, &start); err != nil {
		return err
	}
	minutes, err := parseMinuteValue(value.Duration, strconv.IntSize)
	if err != nil {
		return fmt.Errorf("nfo: video duration=%q: %w", value.Duration, err)
	}
	video.DurationMinutes = int(minutes)
	return nil
}

func parseMinuteValue(value string, bitSize int) (int64, error) {
	text := strings.TrimSpace(value)
	if text == "" {
		return 0, nil
	}
	for _, suffix := range []string{"分钟", "分鐘", "分", "minutes", "minute", "mins", "min"} {
		if strings.HasSuffix(strings.ToLower(text), suffix) {
			text = strings.TrimSpace(text[:len(text)-len(suffix)])
			break
		}
	}
	// Reject unknown text, decimal values and overflow rather than guessing a
	// duration or hiding genuinely invalid metadata from the scanner.
	return strconv.ParseInt(text, 10, bitSize)
}
