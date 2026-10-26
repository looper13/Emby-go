package nfo

import (
	"encoding/xml"
	"fmt"
	"math"
	"strconv"
	"strings"
)

// Some scrapers put a minute suffix or H:MM:SS in runtime/duration. Decode
// those fields separately while keeping numeric fields and generated NFOs unchanged.
func (m *MovieMeta) UnmarshalXML(decoder *xml.Decoder, start xml.StartElement) error {
	type plain MovieMeta
	value := struct {
		*plain
		Runtime string `xml:"runtime"`
	}{plain: (*plain)(m)}
	if err := decoder.DecodeElement(&value, &start); err != nil {
		return err
	}
	minutes, seconds, err := parseDurationValue(value.Runtime, 64)
	if err != nil {
		return fmt.Errorf("nfo: runtime=%q: %w", value.Runtime, err)
	}
	m.Runtime = minutes
	m.clockSeconds = seconds
	m.Root = start.Name.Local
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
	minutes, seconds, err := parseDurationValue(value.Duration, strconv.IntSize)
	if err != nil {
		return fmt.Errorf("nfo: video duration=%q: %w", value.Duration, err)
	}
	video.DurationMinutes = int(minutes)
	video.clockSeconds = seconds
	return nil
}

func parseDurationValue(value string, bitSize int) (minutes, clockSeconds int64, err error) {
	text := strings.TrimSpace(value)
	if !strings.Contains(text, ":") {
		minutes, err = parseMinuteValue(text, bitSize)
		return minutes, 0, err
	}
	parts := strings.Split(text, ":")
	if len(parts) != 3 || len(parts[0]) == 0 || len(parts[1]) != 2 || len(parts[2]) != 2 {
		return 0, 0, fmt.Errorf("invalid H:MM:SS duration %q", value)
	}
	for _, part := range parts {
		for _, digit := range part {
			if digit < '0' || digit > '9' {
				return 0, 0, fmt.Errorf("invalid H:MM:SS duration %q", value)
			}
		}
	}
	hours, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil {
		return 0, 0, err
	}
	minutePart, _ := strconv.Atoi(parts[1])
	secondPart, _ := strconv.Atoi(parts[2])
	if minutePart >= 60 || secondPart >= 60 || hours > (math.MaxInt64-int64(minutePart*60+secondPart))/3600 {
		return 0, 0, fmt.Errorf("invalid H:MM:SS duration %q", value)
	}
	clockSeconds = hours*3600 + int64(minutePart*60+secondPart)
	minutes = clockSeconds / 60
	if _, err := strconv.ParseInt(strconv.FormatInt(minutes, 10), 10, bitSize); err != nil {
		return 0, 0, err
	}
	return minutes, clockSeconds, nil
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
