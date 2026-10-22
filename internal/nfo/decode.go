package nfo

import (
	"bytes"
	"encoding/xml"
)

// Vendor extensions must not overwrite ordinary fields by their local name.
// Ordinary NFOs retain a single decoding pass and the existing timing contract.
func decodeMetadata(data []byte, meta *MovieMeta) error {
	if bytes.Contains(data, []byte("xmlns")) {
		text := string(data)
		elements, _, root, err := directElements(text)
		if err != nil {
			return err
		}
		if root.Space == "" {
			for i := len(elements) - 1; i >= 0; i-- {
				if element := elements[i]; element.name.Space != "" {
					text = text[:element.start] + text[element.end:]
				}
			}
			data = []byte(text)
		}
	}
	return xml.Unmarshal(data, meta)
}
