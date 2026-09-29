package shell

import (
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/Nomadcxx/sysc-shell/internal/plugin/store"
	"github.com/Nomadcxx/sysc-shell/plugin/catalog"
)

const maxPluginScreenshotBytes = 2 << 20

type SortKey uint8

const (
	SortName SortKey = iota
	SortUpdated
	SortAdded
	SortCategory
	SortInstalledFirst
)

func (key SortKey) Next() SortKey {
	if key >= SortInstalledFirst {
		return SortName
	}
	return key + 1
}

type BrowseQuery struct {
	Text, Source, Category string
	HideInstalled          bool
	Sort                   SortKey
	Desc                   bool
}

func browseListings(listings []store.Listing, query BrowseQuery) []store.Listing {
	filtered := make([]store.Listing, 0, len(listings))
	needle := strings.ToLower(strings.TrimSpace(query.Text))
	for _, listing := range listings {
		if query.Source != "" && listing.Source != query.Source {
			continue
		}
		if query.Category != "" && listing.Entry.Category != query.Category {
			continue
		}
		if query.HideInstalled && (listing.Installed != nil || listing.LocalDir != "") {
			continue
		}
		if needle != "" && !containsFold(listing.Entry.Name, needle) &&
			!containsFold(listing.Entry.Author, needle) && !containsFold(listing.Entry.Description, needle) {
			continue
		}
		filtered = append(filtered, listing)
	}
	sort.SliceStable(filtered, func(i, j int) bool {
		a, b := filtered[i], filtered[j]
		var order int
		switch query.Sort {
		case SortUpdated:
			order = compareTimes(a.Entry.UpdatedAt, b.Entry.UpdatedAt, query.Desc)
		case SortAdded:
			order = compareTimes(a.Entry.AddedAt, b.Entry.AddedAt, query.Desc)
		case SortCategory:
			order = compareText(a.Entry.Category, b.Entry.Category)
			if query.Desc {
				order = -order
			}
		case SortInstalledFirst:
			aInstalled := a.Installed != nil || a.LocalDir != ""
			bInstalled := b.Installed != nil || b.LocalDir != ""
			if aInstalled != bInstalled {
				if aInstalled {
					return true
				}
				return false
			}
		default:
			order = compareText(a.Entry.Name, b.Entry.Name)
			if query.Desc {
				order = -order
			}
		}
		if order != 0 {
			return order < 0
		}
		return listingTieLess(a, b)
	})
	return filtered
}

func containsFold(value, needle string) bool {
	return strings.Contains(strings.ToLower(value), needle)
}

func compareText(a, b string) int {
	return strings.Compare(strings.ToLower(a), strings.ToLower(b))
}

func compareTimes(a, b time.Time, descending bool) int {
	if a.IsZero() != b.IsZero() {
		if a.IsZero() {
			return 1
		}
		return -1
	}
	if a.IsZero() {
		return 0
	}
	switch {
	case a.Before(b):
		if descending {
			return 1
		}
		return -1
	case a.After(b):
		if descending {
			return -1
		}
		return 1
	default:
		return 0
	}
}

func listingTieLess(a, b store.Listing) bool {
	if name := compareText(a.Entry.Name, b.Entry.Name); name != 0 {
		return name < 0
	}
	return compareText(a.Source+"/"+a.Entry.ID, b.Source+"/"+b.Entry.ID) < 0
}

type BadgeTone string

const (
	BadgeOfficial  BadgeTone = "official"
	BadgeCommunity BadgeTone = "community"
	BadgeSource    BadgeTone = "source"
	BadgeWarning   BadgeTone = "warning"
	BadgeLocal     BadgeTone = "local"
)

type Badge struct {
	Label string
	Tone  BadgeTone
}

type StoreAction string

const (
	ActionInstall   StoreAction = "install"
	ActionInstalled StoreAction = "installed"
	ActionUpdate    StoreAction = "update"
	ActionDisabled  StoreAction = "disabled"
	ActionRemove    StoreAction = "remove"
)

type Card struct {
	Title          string
	Byline         string
	Description    string
	Badges         []Badge
	Screenshot     store.MediaKey
	ScreenshotPath string
	Glyph          string
	Action         StoreAction
	Reason         string
}

func cardFor(listing store.Listing, media map[string]store.MediaState) Card {
	version := listing.Entry.Version
	if listing.Resolution.Release != nil {
		version = listing.Resolution.Release.Version
	}
	byline := listing.Entry.Author
	if version != "" && byline != "" {
		byline = "v" + version + " · " + byline
	} else if version != "" {
		byline = "v" + version
	}
	card := Card{
		Title: listing.Entry.Name, Byline: byline, Description: listing.Entry.Description,
		Badges: listingBadges(listing), Glyph: categoryGlyph(listing.Entry.Category),
	}
	if listing.Entry.Screenshot != nil {
		card.Screenshot = store.MediaKey{
			URL: listing.Entry.Screenshot.URL, SHA256: listing.Entry.Screenshot.SHA256,
			Max: maxPluginScreenshotBytes,
		}
		if cached, ok := media[card.Screenshot.SHA256]; ok && cached.Err == nil {
			card.ScreenshotPath = cached.Path
		}
	}
	card.Action, card.Reason = cardAction(listing)
	return card
}

func listingBadges(listing store.Listing) []Badge {
	var badges []Badge
	switch listing.Source {
	case "sysc":
		badges = append(badges, Badge{Label: "Official", Tone: BadgeOfficial})
	case "community":
		badges = append(badges, Badge{Label: "Community", Tone: BadgeCommunity})
	case "":
	default:
		badges = append(badges, Badge{Label: listing.Source, Tone: BadgeSource})
	}
	if listing.Entry.Deprecated {
		badges = append(badges, Badge{Label: "Deprecated", Tone: BadgeWarning})
	}
	if listing.Status == store.StatusHeldBack || listing.Resolution.Compat == store.HeldBack {
		badges = append(badges, Badge{Label: "Held back", Tone: BadgeWarning})
	}
	if listing.Installed != nil && listing.LocalDir != "" {
		badges = append(badges, Badge{Label: "Local override", Tone: BadgeLocal})
	}
	return badges
}

func cardAction(listing store.Listing) (StoreAction, string) {
	if listing.UpdateAvailable {
		return ActionUpdate, ""
	}
	switch listing.Status {
	case store.StatusAvailable:
		return ActionInstall, ""
	case store.StatusInstalled, store.StatusShadowed:
		return ActionInstalled, ""
	case store.StatusHeldBack:
		return ActionDisabled, protocolReason(listing)
	case store.StatusIncompatible:
		if listing.Resolution.NoAsset {
			return ActionDisabled, "no release for this machine"
		}
		return ActionDisabled, protocolReason(listing)
	case store.StatusLocalOnly:
		return ActionDisabled, "local copy"
	case store.StatusUnlisted:
		return ActionDisabled, "not listed by an enabled source"
	default:
		return ActionDisabled, string(listing.Status)
	}
}

func protocolReason(listing store.Listing) string {
	return "needs protocol " + strconv.Itoa(listing.Resolution.Needs.Major) + "." + strconv.Itoa(listing.Resolution.Needs.Minor)
}

var categoryGlyphs = map[string]string{
	"utilities": "widgets", "monitoring": "memory", "system": "desktop_windows",
	"appearance": "palette", "productivity": "schedule", "media": "music_note",
	"audio": "graphic_eq", "networking": "wifi", "weather": "partly_cloudy_day",
	"finance": "balance", "social": "person",
}

func categoryGlyph(category string) string {
	if glyph := categoryGlyphs[category]; glyph != "" {
		return glyph
	}
	return "extension"
}

type Detail struct {
	ID               string
	Title            string
	Author           string
	Category         string
	Glyph            string
	Version          string
	ReleaseRef       store.ReleaseRef
	License          string
	Source           string
	SourceBadge      string
	CatalogCommit    string
	SHA256Prefix     string
	Description      string
	Readme           string
	Homepage         string
	ReleaseNotes     string
	Capabilities     []string
	RequiredCommands []string
	Screenshot       store.MediaKey
	ScreenshotPath   string
	Badges           []Badge
	Action           StoreAction
	Reason           string
	NeedsConsent     bool
	StartImmediately bool
	ConsentLines     []string
}

func detailFor(listing store.Listing, enabled bool, media map[string]store.MediaState, readme string) Detail {
	card := cardFor(listing, media)
	release := listing.Resolution.Release
	if release == nil {
		release = &listing.Entry.Release
	}
	version := release.Version
	if version == "" {
		version = listing.Entry.Version
	}
	sha := release.Assets[listing.Resolution.AssetKey].SHA256
	shaPrefix := sha
	if len(shaPrefix) > 12 {
		shaPrefix = shaPrefix[:12]
	}
	action, reason := detailAction(listing)
	needsConsent := action == ActionInstall
	if action == ActionUpdate && listing.Installed != nil {
		needsConsent = !catalog.SameSet(listing.Installed.Capabilities, release.Capabilities) ||
			!catalog.SameSet(listing.Installed.Requires, release.Requires.Commands)
	}
	description := listing.Entry.LongDescription
	if description == "" {
		description = listing.Entry.Description
	}
	detail := Detail{
		ID: listing.Entry.ID, Title: listing.Entry.Name, Author: listing.Entry.Author,
		Category: listing.Entry.Category, Glyph: categoryGlyph(listing.Entry.Category),
		Version: version, ReleaseRef: store.ReleaseRef{Version: version, SHA256: sha},
		License: listing.Entry.License, Source: listing.Source,
		SourceBadge: badgeLabel(listing.Source), CatalogCommit: listing.CatalogCommit,
		SHA256Prefix: shaPrefix, Description: description, Readme: readme,
		Homepage: listing.Entry.Homepage, ReleaseNotes: release.ReleaseNotes,
		Capabilities:     slices.Clone(release.Capabilities),
		RequiredCommands: slices.Clone(release.Requires.Commands),
		Screenshot:       card.Screenshot, ScreenshotPath: card.ScreenshotPath, Badges: card.Badges,
		Action: action, Reason: reason, NeedsConsent: needsConsent, StartImmediately: enabled,
	}
	if action == ActionInstall || action == ActionUpdate {
		detail.ConsentLines = []string{
			"Source: " + detail.SourceBadge,
			"Version: " + version,
			"Catalog commit: " + listing.CatalogCommit,
			"SHA-256: " + sha,
			"Capabilities: " + strings.Join(detail.Capabilities, ", "),
			"Required commands: " + strings.Join(detail.RequiredCommands, ", "),
			"It runs as your user with full file and network access.",
		}
		if enabled {
			detail.ConsentLines = append(detail.ConsentLines, "It is still enabled, so it will start immediately.")
		}
	}
	return detail
}

func detailAction(listing store.Listing) (StoreAction, string) {
	if listing.UpdateAvailable {
		return ActionUpdate, ""
	}
	if listing.Installed != nil {
		return ActionRemove, ""
	}
	if listing.Status == store.StatusAvailable {
		return ActionInstall, ""
	}
	return cardAction(listing)
}

func badgeLabel(source string) string {
	switch source {
	case "sysc":
		return "Official"
	case "community":
		return "Community"
	default:
		return source
	}
}
