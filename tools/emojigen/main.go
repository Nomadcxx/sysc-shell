// Command emojigen regenerates internal/emoji/emoji.tsv from pinned Unicode
// and CLDR data. It is an authoring tool, never run by go build.
package main

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
)

const (
	testURL    = "https://www.unicode.org/Public/emoji/16.0/emoji-test.txt"
	annURL     = "https://raw.githubusercontent.com/unicode-org/cldr/release-46/common/annotations/en.xml"
	derivedURL = "https://raw.githubusercontent.com/unicode-org/cldr/release-46/common/annotationsDerived/en.xml"
)

type ldml struct {
	Annotations []struct {
		CP    string `xml:"cp,attr"`
		Type  string `xml:"type,attr"`
		Value string `xml:",chardata"`
	} `xml:"annotations>annotation"`
}

func fetch(url, wantSHA string) []byte {
	resp, err := http.Get(url)
	if err != nil {
		log.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		log.Fatalf("%s: %s", url, resp.Status)
	}
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		log.Fatal(err)
	}
	sum := sha256.Sum256(b)
	got := hex.EncodeToString(sum[:])
	fmt.Fprintf(os.Stderr, "%s  %s\n", got, url)
	if wantSHA != "" && got != wantSHA {
		log.Fatalf("%s: sha256 %s, want %s", url, got, wantSHA)
	}
	return b
}

func keywords(b []byte, into map[string][]string) {
	var doc ldml
	if err := xml.Unmarshal(b, &doc); err != nil {
		log.Fatal(err)
	}
	for _, a := range doc.Annotations {
		if a.Type == "tts" {
			continue
		}
		if _, seen := into[a.CP]; seen {
			continue
		}
		var kw []string
		for _, k := range strings.Split(a.Value, "|") {
			if k = strings.TrimSpace(k); k != "" {
				kw = append(kw, k)
			}
		}
		into[a.CP] = kw
	}
}

func skinToned(s string) bool {
	for _, r := range s {
		if r >= 0x1F3FB && r <= 0x1F3FF {
			return true
		}
	}
	return false
}

func main() {
	out := flag.String("out", "internal/emoji/emoji.tsv", "output TSV")
	testSHA := flag.String("sha-test", "", "expected sha256 of emoji-test.txt")
	annSHA := flag.String("sha-ann", "", "expected sha256 of annotations/en.xml")
	derivedSHA := flag.String("sha-derived", "", "expected sha256 of annotationsDerived/en.xml")
	flag.Parse()

	kw := map[string][]string{}
	keywords(fetch(annURL, *annSHA), kw)
	keywords(fetch(derivedURL, *derivedSHA), kw)

	var buf bytes.Buffer
	group, rows := "", 0
	sc := bufio.NewScanner(bytes.NewReader(fetch(testURL, *testSHA)))
	for sc.Scan() {
		line := sc.Text()
		if g, ok := strings.CutPrefix(line, "# group: "); ok {
			group = g
			continue
		}
		if line == "" || strings.HasPrefix(line, "#") || group == "Component" {
			continue
		}
		fields, comment, ok := strings.Cut(line, "#")
		if !ok || !strings.Contains(fields, "; fully-qualified") {
			continue
		}
		// comment: " 😀 E1.0 grinning face"
		parts := strings.Fields(comment)
		if len(parts) < 3 || !strings.HasPrefix(parts[1], "E") {
			continue
		}
		char, name := parts[0], strings.Join(parts[2:], " ")
		if skinToned(char) {
			continue
		}
		// CLDR keys most sequences without the U+FE0F variation selector
		// that the fully-qualified form carries.
		ann, ok := kw[char]
		if !ok {
			ann = kw[strings.ReplaceAll(char, "\uFE0F", "")]
		}
		var keep []string
		for _, k := range ann {
			if !strings.EqualFold(k, name) {
				keep = append(keep, k)
			}
		}
		fmt.Fprintf(&buf, "%s\t%s\t%s\n", char, name, strings.Join(keep, "|"))
		rows++
	}
	if err := sc.Err(); err != nil {
		log.Fatal(err)
	}
	if err := os.WriteFile(*out, buf.Bytes(), 0o644); err != nil {
		log.Fatal(err)
	}
	fmt.Fprintf(os.Stderr, "%d rows -> %s\n", rows, *out)
}
