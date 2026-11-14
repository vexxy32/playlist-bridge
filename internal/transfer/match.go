package transfer

import (
	"regexp"
	"slices"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

var remasterSuffix = regexp.MustCompile(`(?i)\s*(?:[-–—:|]\s*|[(\[]\s*)(?:(\d{4})\s+)?remaster(?:ed)?(?:\s+(\d{4}))?\s*[)\]]?\s*$`)
var bracketedFeature = regexp.MustCompile(`(?i)[(\[]\s*(?:feat\.?|ft\.?|featuring)\s+([^\])]+)[)\]]`)
var featureMarker = regexp.MustCompile(`(?i)\s+(?:feat\.?|ft\.?|featuring)\s+`)
var featureSuffix = regexp.MustCompile(`[(\[]|\s+-\s+|[–—|]`)
var artistSeparators = regexp.MustCompile(`(?i)\s*,\s*|\s+&\s+|\s+and\s+|\s+(?:x|vs\.?)\s+`)
var cleanSuffix = regexp.MustCompile(`(?i)\s*(?:[-–—:|]\s*|[(\[]\s*)clean(?:\s+(?:ver\.?|version|edit))?\s*[)\]]?\s*$`)
var explicitSuffix = regexp.MustCompile(`(?i)\s*(?:[-–—:|]\s*|[(\[]\s*)explicit(?:\s+(?:ver\.?|version|edit))?\s*[)\]]?\s*$`)
var videoProductionCredit = regexp.MustCompile(`^(?:directed by .+|(?:official )?(?:studio )?(?:music )?(?:video|mv|movie)|video oficial|official dance video|vertical video|a visual|us version|клип|音源|hq|original (?:audio|recording)|studio recording|archive upload|from the (?:archive|listening room)|full song from .+|tiktok (?:19|20)\d{2} viral hit)$`)
var videoCreditSeparator = regexp.MustCompile(`\s+(?:-|/{1,2}|[•·])\s+|\s*[–—:|｜]\s*`)
var videoBraceLabel = regexp.MustCompile(`(?i)\s+\{\s*HQ\s*\}`)
var trailingVideoCredit = regexp.MustCompile(`(?i)\s+(?:official\s+(?:music\s+)?(?:video|audio)|lyric\s+video|archive upload|клип|音源)\s*$`)
var bracketedWith = regexp.MustCompile(`(?i)^with\s+(.+)$`)
var withCredit = regexp.MustCompile(`(?i)\s+(?:with|w[./])\s+`)
var titleBrackets = regexp.MustCompile(`[(\[]([^()\[\]]+)[)\]]`)
var titleSeparator = regexp.MustCompile(`\s+-\s+|\s*[–—:|]\s*`)
var neutralTitleLabel = regexp.MustCompile(`^(?:single(?: version| edit)?|radio (?:edit|version|mix)|original (?:mix|version)|album version|official(?: (?:music video|audio|lyric video|video|hd video|4k video))?|(?:music|lyric|lyrics) video|lyrics|audio|hd|4k|(?:19|20)\d{2}|(?:original )?(?:motion picture )?soundtrack(?: version)?|ost|(?:ost|soundtrack) .+|(?:from|as featured in) .+|.+ (?:ost|soundtrack))$`)
var versionWord = regexp.MustCompile(`\b(live|en vivo|remix|cover|re recording|rerecording|instrumental|karaoke|acoustic|stripped|demo|speed up|sped up|slowed|nightcore)\b`)
var bareVersion = regexp.MustCompile(`^(live(?: (?:at|from|in|on) .+| version)?|remix(?: by .+)?|cover(?: by .+| version)?|instrumental(?: version)?|karaoke(?: version)?|(?:acoustic|stripped|demo)(?: version)?|speed up(?: version)?|sped up(?: version)?|slowed(?:(?: and)? reverb)?|nightcore)$`)
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
	explicit bool
	radio    bool
	versions []string
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
	title = titleBrackets.ReplaceAllStringFunc(title, func(label string) string {
		words := strings.Join(titleWords(label), " ")
		if isNeutralTitleLabel(words) && title != label {
			parsed.radio = parsed.radio || strings.HasPrefix(words, "radio ")
			return " "
		}
		if len(versionKinds(words)) > 0 && title != label {
			parsed.versions = append(parsed.versions, words)
			return " "
		}
		return label
	})
	for title = cleanSpacing(title); title != ""; title = cleanSpacing(title) {
		if cleanSuffix.MatchString(title) {
			parsed.clean = true
			title = cleanSuffix.ReplaceAllString(title, "")
			continue
		}
		if parts := remasterSuffix.FindStringSubmatch(title); parts != nil {
			parsed.remaster = true
			parsed.year = parts[1] + parts[2]
			title = strings.TrimSuffix(title, parts[0])
			continue
		}
		base, label := titleEdition(title)
		words := strings.Join(titleWords(label), " ")
		if isNeutralTitleLabel(words) {
			parsed.radio = parsed.radio || strings.HasPrefix(words, "radio ")
			title = base
			continue
		}
		if len(versionKinds(words)) == 0 {
			break
		}
		parsed.versions = append(parsed.versions, words)
		title = base
	}
	parsed.base = title
	return parsed
}

// titleEdition извлекает пометку версии в конце названия.
func titleEdition(title string) (base, label string) {
	end := len(title)
	var bracketBase, bracketLabel string
	if labels := titleBrackets.FindAllStringSubmatchIndex(title, -1); len(labels) > 0 {
		last := labels[len(labels)-1]
		if last[0] > 0 && last[1] == len(title) {
			end = last[0]
			bracketBase, bracketLabel = title[:last[0]], title[last[2]:last[3]]
		}
	}
	separators := titleSeparator.FindAllStringIndex(title[:end], -1)
	for i := len(separators) - 1; i >= 0; i-- {
		split := separators[i]
		label := title[split[1]:]
		if len(versionKinds(strings.Join(titleWords(label), " "))) > 0 {
			return title[:split[0]], label
		}
	}
	if bracketLabel != "" {
		return bracketBase, bracketLabel
	}
	if len(separators) > 0 {
		last := separators[len(separators)-1]
		return title[:last[0]], title[last[1]:]
	}
	return title, ""
}

func titleWords(title string) []string {
	title = strings.ReplaceAll(matchText(title), "'", "")
	var text strings.Builder
	latin := false
	for _, r := range norm.NFD.String(title) {
		if unicode.IsMark(r) && latin {
			continue
		}
		latin = unicode.In(r, unicode.Latin)
		text.WriteRune(r)
	}
	return strings.FieldsFunc(text.String(), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsNumber(r) && !unicode.IsMark(r)
	})
}

func isNeutralTitleLabel(label string) bool {
	return neutralTitleLabel.MatchString(label) && !versionWord.MatchString(label)
}

func versionKinds(label string) []string {
	var kinds []string
	for _, word := range versionWord.FindAllString(label, -1) {
		switch word {
		case "sped up", "nightcore":
			word = "speed up"
		case "re recording":
			word = "rerecording"
		case "en vivo":
			word = "live"
		}
		kinds = append(kinds, word)
	}
	if len(kinds) == 0 && strings.HasSuffix(label, " mix") {
		kinds = append(kinds, "mix")
	}
	return kinds
}

// matchVersions учитывает суффиксы версии, если они дополняют другое название.
func matchVersions(want, got trackTitle, wantWords, gotWords []string) ([]string, []string, bool) {
	wantLabel, gotLabel := strings.Join(wantWords, " "), strings.Join(gotWords, " ")
	if extra, ok := strings.CutPrefix(gotLabel, wantLabel+" "); ok && isBareVersion(extra) {
		got.versions = append(got.versions, extra)
		gotWords = wantWords
	}
	if extra, ok := strings.CutPrefix(wantLabel, gotLabel+" "); ok && isBareVersion(extra) {
		want.versions = append(want.versions, extra)
		wantWords = gotWords
	}
	return wantWords, gotWords, versionsMatch(want.versions, got.versions) && versionsMatch(got.versions, want.versions)
}

func isBareVersion(label string) bool {
	return bareVersion.MatchString(label) || strings.HasSuffix(label, " remix") || strings.HasSuffix(label, " cover")
}

func versionsMatch(left, right []string) bool {
	for _, label := range left {
		label = strings.TrimSuffix(label, " version")
		for _, kind := range versionKinds(label) {
			matched := false
			for _, other := range right {
				other = strings.TrimSuffix(other, " version")
				// Общая пометка может совпасть с конкретной, но две разные версии должны совпадать.
				if slices.Contains(versionKinds(other), kind) && (label == other ||
					label == kind || other == kind || kind == "speed up" || kind == "slowed") {
					matched = true
					break
				}
			}
			if !matched {
				return false
			}
		}
		if len(versionKinds(label)) == 0 && !slices.ContainsFunc(right, func(other string) bool {
			return label == strings.TrimSuffix(other, " version")
		}) {
			return false
		}
	}
	return true
}

func titleSimilarity(want, got []string) float64 {
	if slices.Equal(want, got) {
		return 1
	}
	wantSet := make(map[string]struct{}, len(want))
	gotSet := make(map[string]struct{}, len(got))
	for _, word := range want {
		wantSet[word] = struct{}{}
	}
	for _, word := range got {
		gotSet[word] = struct{}{}
	}
	common := 0
	for word := range wantSet {
		if _, ok := gotSet[word]; ok {
			common++
		}
	}
	union := len(wantSet) + len(gotSet) - common
	if union == 0 {
		return 0
	}
	similarity := float64(common) / float64(union)
	if similarity == 1 && len(want) == len(got) && len(want) > 2 {
		// Одинаковые слова в другом порядке могут обозначать разные песни.
		return .78
	}
	if similarity == 1 {
		return 0
	}
	return similarity
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

func artistIndex(name string, artists []string) int {
	for i, artist := range artists {
		if creditMatches(name, artist) {
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

func matchingTitle(parsed trackTitle, artists []string) trackTitle {
	if explicitSuffix.MatchString(parsed.base) {
		parsed.explicit = true
		parsed.base = strings.TrimSpace(explicitSuffix.ReplaceAllString(parsed.base, ""))
	}
	base, label := titleEdition(parsed.base)
	words := titleWords(label)
	if len(words) > 1 && (words[len(words)-1] == "ver" || words[len(words)-1] == "version") {
		parsed.base = strings.TrimSpace(base)
		parsed.versions = append(slices.Clone(parsed.versions), strings.Join(words[:len(words)-1], " ")+" version")
	}
	base, label, ok := trailingTitleBracket(parsed.base)
	if !ok {
		return parsed
	}
	if credit := bracketedWith.FindStringSubmatch(label); credit != nil && guestCreditsMatch(credit[1:], artists) {
		parsed.base = base
		parsed.guests = append(slices.Clone(parsed.guests), credit[1])
	} else if names := artistSeparators.Split(label, -1); len(names) > 1 && allArtistsMatch(names, artists) {
		parsed.base = base
	}
	return parsed
}

func trailingTitleBracket(title string) (string, string, bool) {
	brackets := titleBrackets.FindAllStringSubmatchIndex(title, -1)
	if len(brackets) > 0 {
		last := brackets[len(brackets)-1]
		if last[0] > 0 && last[1] == len(title) {
			return strings.TrimSpace(title[:last[0]]), strings.TrimSpace(title[last[2]:last[3]]), true
		}
	}
	return title, "", false
}

func creditMatches(credit, artist string) bool {
	if sameArtist(credit, artist) {
		return true
	}
	base, alias, ok := trailingTitleBracket(credit)
	return ok && alternateScript(base, alias) && sameArtist(alias, artist)
}

func alternateScript(left, right string) bool {
	const latin, cyrillic = 1, 2
	script := func(text string) int {
		kind := 0
		for _, r := range text {
			if unicode.In(r, unicode.Latin) {
				kind |= latin
			}
			if unicode.In(r, unicode.Cyrillic) {
				kind |= cyrillic
			}
		}
		return kind
	}
	leftScript, rightScript := script(left), script(right)
	return (leftScript == latin && rightScript == cyrillic) || (leftScript == cyrillic && rightScript == latin)
}

func withoutVideoProductionCredit(title string) string {
	title = trailingVideoCredit.ReplaceAllString(title, "")
	title = videoBraceLabel.ReplaceAllString(title, "")
	return cleanSpacing(titleBrackets.ReplaceAllStringFunc(title, func(bracket string) string {
		if title != bracket && videoProductionCredit.MatchString(strings.Join(titleWords(bracket), " ")) {
			return " "
		}
		return bracket
	}))
}

func normalizeVideoTitle(want, got trackTitle, album string) ([]string, []string) {
	wantWords, gotWords := titleWords(want.base), titleWords(got.base)
	wantBase, wantLabel, wantBracketed := trailingTitleBracket(want.base)
	gotBase, gotLabel, gotBracketed := trailingTitleBracket(got.base)
	if wantBracketed && album != "" && slices.Equal(titleWords(wantLabel), titleWords(album)) {
		wantWords = titleWords(wantBase)
	} else if wantBracketed && !gotBracketed && alternateScript(wantBase, wantLabel) {
		wantWords = titleWords(wantBase)
	} else if wantBracketed && gotBracketed && alternateScript(wantLabel, gotLabel) &&
		slices.Equal(titleWords(wantBase), titleWords(gotBase)) {
		wantWords, gotWords = titleWords(wantBase), titleWords(gotBase)
	}
	return wantWords, gotWords
}

// videoCredit ищет исполнителя из названия среди исполнителей кандидата.
func videoCredit(title string, song []string, artists []string, channel string) (string, bool) {
	if len(artists) == 0 {
		return "", false
	}
	// Убираем префикс только если он обозначает канал загрузки.
	if prefix, rest, ok := strings.Cut(title, ":"); ok {
		var initials strings.Builder
		for i, r := range []rune(channel) {
			if unicode.IsLetter(r) && (i == 0 || unicode.IsUpper(r)) {
				initials.WriteRune(r)
			}
		}
		prefixKey := artistKey(prefix)
		if sameArtist(prefix, channel) || (len(prefixKey) >= 3 && prefixKey == artistKey(initials.String())) {
			title = strings.TrimSpace(rest)
		}
	}
	separators := videoCreditSeparator.FindAllStringIndex(title, -1)
	for _, split := range separators {
		parts := [2]string{strings.TrimSpace(title[:split[0]]), strings.TrimSpace(title[split[1]:])}
		for i, credit := range parts {
			if !creditMatches(credit, artists[0]) &&
				(len(artistSeparators.Split(credit, -1)) < 2 || !guestCreditsMatch([]string{credit}, artists)) {
				continue
			}
			other := withoutVideoProductionCredit(parts[1-i])
			if slices.Equal(withoutTitleArticle(titleWords(other)), withoutTitleArticle(song)) {
				return other, true
			}
			creditText := strings.ReplaceAll(strings.ReplaceAll(other, " (", "  "), " [", "  ")
			for _, marker := range withCredit.FindAllStringIndex(creditText, -1) {
				// У кандидата должны быть участники, указанные в названии ролика.
				guest := strings.Trim(other[marker[1]:], "()[] ")
				base := strings.TrimRight(other[:marker[0]], " ([")
				if slices.Equal(withoutTitleArticle(titleWords(base)), withoutTitleArticle(song)) &&
					guestCreditsMatch([]string{guest}, artists) {
					return base, true
				}
			}
		}
	}
	return "", false
}

func withoutTitleArticle(words []string) []string {
	if len(words) > 1 && words[0] == "the" {
		return words[1:]
	}
	return words
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

func normalizeISRC(isrc string) string {
	return strings.ToUpper(strings.Map(func(r rune) rune {
		if r == '-' || unicode.IsSpace(r) {
			return -1
		}
		return r
	}, isrc))
}

func durationSimilarity(want, got int) float64 {
	diff := max(want, got) - min(want, got)
	switch {
	case diff <= 3000:
		return 1
	case diff <= 15000:
		return 1 - .5*float64(diff-3000)/12000
	default:
		return max(0, .5*float64(30000-diff)/15000)
	}
}

// embeddedVideoCredit сравнивает название ролика с исполнителем и треком в любом порядке.
func embeddedVideoCredit(video, song []string, artist string) bool {
	videoText := strings.Join(video, " ")
	artistText := strings.Join(titleWords(artist), " ")
	if videoText == "" || artistText == "" || len(song) == 0 {
		return false
	}
	withoutArticle := func(text string) string { return strings.TrimPrefix(text, "the ") }
	songText := withoutArticle(strings.Join(song, " "))
	artistText = withoutArticle(artistText)
	for _, name := range []string{artistText, "the " + artistText} {
		if tail, ok := strings.CutPrefix(videoText, name+" "); ok && withoutArticle(tail) == songText {
			return true
		}
		if head, ok := strings.CutSuffix(videoText, " "+name); ok &&
			withoutArticle(strings.TrimSuffix(head, " by")) == songText {
			return true
		}
	}
	return false
}

// selectCandidate оценивает доступные признаки и выбирает кандидата выше порога.
// Взвешенная оценка учитывает положительные признаки, даже если
// у других кандидатов это поле отсутствует.
func selectCandidate(track Track, candidates []Candidate) (Candidate, bool) {
	const threshold = .80
	want := parseTitle(track.Title)
	artist := parseTitle(track.Artist)
	artistCredit := artist.base
	want.guests = append(want.guests, artist.guests...)
	wantWords := titleWords(want.base)
	wantAlbum, wantISRC := matchText(track.Album), normalizeISRC(track.ISRC)
	if len(wantWords) == 0 || (artistCredit == "" && track.videoSearchTitle == "") {
		return Candidate{}, false
	}
	videoTitle := parseTitle(track.videoSearchTitle)
	videoWords := titleWords(videoTitle.base)
	if len(videoTitle.versions) > 0 && len(want.versions) == 0 {
		want.versions = videoTitle.versions
	}
	bestScore := -1.0
	var best Candidate
	var exact Candidate
	exactScore := -1.0
	for _, candidate := range candidates {
		source := matchingTitle(want, candidate.Artists)
		if candidate.URI == "" {
			continue
		}
		got := matchingTitle(parseTitle(candidate.Title), candidate.Artists)
		gotWords := titleWords(got.base)
		gotISRC := normalizeISRC(candidate.ISRC)
		sameRecording := wantISRC != "" && wantISRC == gotISRC
		if wantISRC != "" && gotISRC != "" && !sameRecording {
			continue
		}
		if len(gotWords) == 0 || (!sameRecording && want.year != "" && want.year != got.year) {
			continue
		}
		if want.clean && !got.clean && (candidate.Explicit == nil || *candidate.Explicit) {
			continue
		}
		if source.explicit && (got.clean || (candidate.Explicit != nil && !*candidate.Explicit)) {
			continue
		}

		titleWordsToMatch := titleWords(source.base)
		embeddedArtist := false
		if track.fromYouTube {
			source.base = withoutVideoProductionCredit(source.base)
			videoBase := withoutVideoProductionCredit(videoTitle.base)
			if videoBase == "" {
				videoBase = source.base
			}
			if song, ok := videoCredit(videoBase, gotWords, candidate.Artists, track.Artist); ok {
				source.base = song
				embeddedArtist = true
			}
			for _, marker := range withCredit.FindAllStringIndex(source.base, -1) {
				if slices.Equal(titleWords(source.base[:marker[0]]), gotWords) &&
					guestCreditsMatch([]string{source.base[marker[1]:]}, candidate.Artists) {
					source.base = source.base[:marker[0]]
					break
				}
			}
			titleWordsToMatch, gotWords = normalizeVideoTitle(source, got, candidate.Album)
		}
		if !embeddedArtist && len(candidate.Artists) > 0 && embeddedVideoCredit(videoWords, gotWords, candidate.Artists[0]) {
			titleWordsToMatch = gotWords
			embeddedArtist = true
		}
		wantBase, gotBase, matchingVersions := matchVersions(source, got, titleWordsToMatch, gotWords)
		edit := false
		if len(want.versions) == 0 {
			switch {
			case len(got.versions) == 0:
				base, label := titleEdition(got.base)
				edit = creditedEdit(matchText(want.base), matchText(base)+" - "+matchText(label), want.guests)
			case len(got.versions) == 1 && slices.Equal(versionKinds(got.versions[0]), []string{"mix"}):
				edit = creditedEdit(matchText(want.base), matchText(got.base)+" - "+got.versions[0], want.guests)
			}
		}
		if !matchingVersions && !edit {
			continue
		}
		title := titleSimilarity(wantBase, gotBase)
		if sameRecording && matchingVersions {
			// Совпадение ID записи важнее различий оформления и пометок релиза.
			score := title
			if wantAlbum != "" && matchText(candidate.Album) == wantAlbum {
				score += 1
			}
			if score > exactScore {
				exact, exactScore = candidate, score
			}
			continue
		}

		if edit {
			title = .78
		}
		if title < 2.0/3 {
			continue
		}

		artists := candidate.Artists
		mainIndex := artistIndex(artistCredit, artists)
		if embeddedArtist || (mainIndex < 0 && len(artists) > 0 && videoArtistAliasMatches(track.videoArtistAlias, artists[0])) {
			mainIndex = 0
		}
		if mainIndex < 0 {
			// YouTube может указывать нескольких исполнителей одной строкой.
			if len(artists) == 1 {
				parsed := parseTitle(artists[0])
				if sameArtist(artistCredit, parsed.base) {
					artists = append([]string{parsed.base}, parsed.guests...)
				} else {
					artists = append(artistSeparators.Split(parsed.base, -1), parsed.guests...)
				}
			}
			mainIndex = artistIndex(artistCredit, artists)
		}
		if mainIndex > 0 && len(want.guests) == 0 {
			// Один из указанных исполнителей должен быть основным, а не гостем в кавере.
			continue
		}
		if mainIndex < 0 {
			names := artistSeparators.Split(artistCredit, -1)
			if !allArtistsMatch(names, artists) {
				continue
			}
			mainIndex = artistIndex(names[0], artists)
		}
		if edit && mainIndex != 0 {
			continue
		}
		credits := append([]string(nil), artists...)
		for _, guest := range got.guests {
			credits = append(credits, guest)
			credits = append(credits, artistSeparators.Split(guest, -1)...)
		}
		if !guestCreditsMatch(source.guests, credits) {
			continue
		}

		artistScore := 1.0
		if mainIndex > 0 {
			artistScore = .88
		}
		score := 60*(title-threshold) + 25*(artistScore-threshold)
		if album := matchText(candidate.Album); wantAlbum != "" && album != "" {
			if album == wantAlbum {
				score += 7 * (1 - threshold)
			} else {
				score += 7 * (.65 - threshold)
			}
		}
		if track.DurationMs > 0 && candidate.DurationMs > 0 {
			score += 22 * (durationSimilarity(track.DurationMs, candidate.DurationMs) - threshold)
		}
		if score < 0 {
			continue
		}
		if matchText(track.Title) == matchText(candidate.Title) {
			score++
		}
		if want.remaster == got.remaster {
			score += .5
		}
		if want.radio == got.radio {
			score += 1
		}
		if want.clean == got.clean {
			score += .5
		}
		if source.explicit == got.explicit || (candidate.Explicit != nil && *candidate.Explicit) {
			score += .5
		}
		if len(want.guests) == 0 && len(artists) == 1 && len(got.guests) == 0 {
			score += .25
		}
		if score > bestScore {
			best, bestScore = candidate, score
		}
	}
	if exactScore >= 0 {
		return exact, true
	}
	return best, bestScore >= 0
}
