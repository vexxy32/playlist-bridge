package transfer

import (
	"regexp"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

var remasterSuffix = regexp.MustCompile(`(?i)\s*(?:[-–—]\s*|[(\[]\s*)(?:(\d{4})\s+)?remaster(?:ed)?(?:\s+(\d{4}))?\s*[)\]]?\s*$`)
var bracketedFeature = regexp.MustCompile(`(?i)[(\[]\s*(?:feat\.?|ft\.?|featuring)\s+([^\])]+)[)\]]`)
var featureMarker = regexp.MustCompile(`(?i)\s+(?:feat\.?|ft\.?|featuring)\s+`)
var featureSuffix = regexp.MustCompile(`[(\[]|\s+[-–—]\s+`)
var artistSeparators = regexp.MustCompile(`(?i)\s*,\s*|\s+&\s+|\s+and\s+`)
var cleanSuffix = regexp.MustCompile(`(?i)\s*(?:-\s*|[(\[]\s*)clean(?:\s+(?:version|edit))?\s*[)\]]?\s*$`)
var editionBrackets = regexp.MustCompile(`\s+[(\[]([^()\[\]]+)[)\]]$`)
var typography = strings.NewReplacer("’", "'", "‘", "'", "–", "-", "—", "-", "‑", "-", "…", "...")

func cleanSpacing(text string) string {
	return strings.Join(strings.Fields(text), " ")
}

func matchText(text string) string {
	return strings.ToLower(cleanSpacing(typography.Replace(text)))
}

type trackTitle struct {
	base     string
	guests   []string
	remaster bool
	year     string
	clean    bool
}

func parseTitle(title string) trackTitle {
	title = cleanSpacing(title)
	var parsed trackTitle
	// Убираем пометки о гостях из названия, сохраняя имена для проверки.
	title = bracketedFeature.ReplaceAllStringFunc(title, func(credit string) string {
		parsed.guests = append(parsed.guests, strings.TrimSpace(bracketedFeature.FindStringSubmatch(credit)[1]))
		return " "
	})
	if marker := featureMarker.FindStringIndex(title); marker != nil {
		credits := title[marker[1]:]
		suffix := ""
		if ending := featureSuffix.FindStringIndex(credits); ending != nil {
			credits, suffix = credits[:ending[0]], credits[ending[0]:]
		}
		for _, credit := range featureMarker.Split(credits, -1) {
			parsed.guests = append(parsed.guests, strings.TrimRight(strings.TrimSpace(credit), ",;"))
		}
		title = title[:marker[0]] + " " + suffix
	}
	title = cleanSpacing(title)
	parsed.clean = cleanSuffix.MatchString(title)
	title = cleanSuffix.ReplaceAllString(title, "")
	if parts := remasterSuffix.FindStringSubmatch(title); parts != nil {
		title = strings.TrimSpace(strings.TrimSuffix(title, parts[0]))
		parsed.remaster = true
		parsed.year = parts[1] + parts[2]
	}
	parsed.base = cleanSpacing(title)
	return parsed
}

func sameArtist(left, right string) bool {
	left = artistKey(left)
	right = artistKey(right)
	return left != "" && left == right
}

func artistKey(name string) string {
	name = strings.TrimPrefix(matchText(name), "the ")
	var key strings.Builder
	latin := false
	for _, r := range norm.NFD.String(name) {
		if unicode.IsMark(r) {
			if !latin {
				key.WriteRune(r)
			}
			continue
		}
		latin = unicode.In(r, unicode.Latin)
		switch {
		case unicode.IsLetter(r), unicode.IsNumber(r):
			key.WriteRune(r)
		case r == '$':
			key.WriteByte('s')
		}
	}
	return key.String()
}

func editionTitle(title string) string {
	return editionBrackets.ReplaceAllString(matchText(title), " - $1")
}

func artistIndex(name string, artists []string) int {
	for i, artist := range artists {
		if sameArtist(name, artist) {
			return i
		}
	}
	return -1
}

func allArtistsMatch(names, credits []string) bool {
	for _, name := range names {
		if artistIndex(name, credits) < 0 {
			return false
		}
	}
	return true
}

func guestCreditsMatch(guests, credits []string) bool {
	for _, guest := range guests {
		if artistIndex(guest, credits) >= 0 {
			continue
		}
		if !allArtistsMatch(artistSeparators.Split(guest, -1), credits) {
			return false
		}
	}
	return true
}

// creditedEdit допускает явно подписанную редакцию фита при совпадении участников.
func creditedEdit(want, candidate string, guests []string) bool {
	if len(guests) == 0 {
		return false
	}
	label, ok := strings.CutPrefix(candidate, want+" - ")
	if !ok {
		return false
	}
	if label == "final edit version" {
		return true
	}
	name, ok := strings.CutSuffix(label, " mix")
	return ok && artistIndex(name, guests) >= 0
}

// selectCandidate сравнивает музыкальные названия и проверяет заявленных исполнителей.
func selectCandidate(track Track, candidates []Candidate) (Candidate, bool) {
	want := parseTitle(track.Title)
	wantTitle, wantAlbum := editionTitle(want.base), matchText(track.Album)
	if wantTitle == "" || strings.TrimSpace(track.Artist) == "" {
		return Candidate{}, false
	}
	bestScore := -1
	var best Candidate
	for _, candidate := range candidates {
		if candidate.URI == "" {
			continue
		}
		got := parseTitle(candidate.Title)
		gotTitle := editionTitle(got.base)
		edit := creditedEdit(wantTitle, gotTitle, want.guests)
		if (gotTitle != wantTitle && !edit) || (want.year != "" && want.year != got.year) {
			continue
		}
		if want.clean && !got.clean && (candidate.Explicit == nil || *candidate.Explicit) {
			continue
		}
		mainIndex := artistIndex(track.Artist, candidate.Artists)
		if mainIndex > 0 && len(want.guests) == 0 {
			// Один указанный исполнитель должен быть основным, а не гостем в чужой версии.
			continue
		}
		if mainIndex < 0 {
			primary := artistSeparators.Split(track.Artist, -1)
			if len(primary) == 1 || !allArtistsMatch(primary, candidate.Artists) {
				continue
			}
			mainIndex = artistIndex(primary[0], candidate.Artists)
		}
		credits := append([]string(nil), candidate.Artists...)
		for _, guest := range got.guests {
			credits = append(credits, guest)
			credits = append(credits, artistSeparators.Split(guest, -1)...)
		}
		if !guestCreditsMatch(want.guests, credits) || (edit && mainIndex != 0) {
			continue
		}
		score := 0
		if !edit {
			score += 16
		}
		if matchText(track.Title) == matchText(candidate.Title) {
			score += 8
		}
		if want.remaster == got.remaster {
			score += 4
		}
		if mainIndex == 0 {
			score += 16
		}
		if len(want.guests) == 0 && len(candidate.Artists) == 1 && len(got.guests) == 0 {
			score += 2
		}
		if wantAlbum != "" && matchText(candidate.Album) == wantAlbum {
			score += 4
		}
		if score > bestScore {
			best, bestScore = candidate, score
		}
	}
	return best, bestScore >= 0
}
