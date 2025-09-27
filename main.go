package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/html"
)

// ========================================
// 設定と定数
// ========================================

// Config アプリケーション設定
type Config struct {
	DefaultJSONFile string
	BaseURL         string
	MaxRetries      int
	BaseDelay       time.Duration
	RequestDelay    time.Duration
	HTTPTimeout     time.Duration
}

// ParsingRules HTML解析用のルール
type ParsingRules struct {
	TacticCategories []string
	SkillCategories  []string
	InterestItems    []string
	PersonalityTypes []string
	FameTypes        []string
	StrategyTypes    []string
	InterestWidths   []string
	ExcludeTexts     []string
	RetryErrors      []string
	BasicInfoHeaders []string
	AbilityHeaders   []string
	StatusHeaders    []string
	TalentHeaders    []string
	TacticsHeaders   []string
	SkillsHeaders    []string
}

var (
	config = Config{
		DefaultJSONFile: "characters.json",
		BaseURL:         "https://wikiwiki.jp/sangokushi8r/",
		MaxRetries:      3,
		BaseDelay:       2 * time.Second,
		RequestDelay:    500 * time.Millisecond,
		HTTPTimeout:     30 * time.Second,
	}

	rules = ParsingRules{
		TacticCategories: []string{"歩兵", "騎兵", "弓兵", "艦船", "軍略", "補助", "遁甲"},
		SkillCategories:  []string{"任務", "智謀", "兵科", "軍事"},
		InterestItems:    []string{"武具", "書物", "宝物", "茶器", "名馬", "美術", "酒", "音楽", "詩歌", "絵画", "香", "薬草"},
		PersonalityTypes: []string{"豪胆", "冷静", "剛胆", "沈着", "猪突", "温和", "臆病"},
		FameTypes:        []string{"無関心", "重視", "文武不問", "武名", "高名"},
		StrategyTypes:    []string{"好戦", "普通", "積極", "消極", "私欲"},
		InterestWidths:   []string{"60px", "53px", "52px", "51px", "50px"},
		ExcludeTexts:     []string{"ー", "", "興味", "-"},
		RetryErrors:      []string{"429", "Too Many Requests"},
		BasicInfoHeaders: []string{"字", "没年"},
		AbilityHeaders:   []string{"統率", "武力"},
		StatusHeaders:    []string{"重視名声", "物欲", "戦略傾向"},
		TalentHeaders:    []string{"奇才"},
		TacticsHeaders:   []string{"戦法"},
		SkillsHeaders:    []string{"特技"},
	}
)

// ========================================
// エラー型定義
// ========================================

// ProcessingError 処理エラーの詳細情報
type ProcessingError struct {
	URL     string
	Message string
	Err     error
}

func (e *ProcessingError) Error() string {
	return fmt.Sprintf("URL %s の処理エラー: %s", e.URL, e.Message)
}

func (e *ProcessingError) Unwrap() error {
	return e.Err
}

// ========================================
// 構造体定義
// ========================================

// Character 武将の情報を格納する構造体
type Character struct {
	Name         string `json:"名前"`
	Reading      string `json:"読み"`
	Azana        string `json:"字"`
	Leadership   int    `json:"統率"`
	Force        int    `json:"武力"`
	Intelligence int    `json:"知力"`
	Politics     int    `json:"政治"`
	Charm        int    `json:"魅力"`
	Talent       string `json:"奇才"`
	Interest     string `json:"興味"`
	Greed        string `json:"物欲"`
	Loyalty      int    `json:"義理"`
	Personality  string `json:"性格"`
	Strategy     string `json:"戦略傾向"`
	DeathYear    int    `json:"没年"`
	DeathMinus13 int    `json:"没年-13"`
	Tactics      string `json:"戦法"`
	Skills       string `json:"特技"`
	Fame         string `json:"重視名声"`
}

// ========================================
// メイン処理
// ========================================

func main() {
	category, jsonFile := getCategoryAndFile()
	characters := processCategory(category, jsonFile)
	outputJSON(characters)
}

func getCategoryAndFile() (string, string) {
	if len(os.Args) < 2 {
		showAvailableCategories(config.DefaultJSONFile)
		log.Fatal("使用方法: go run main.go <カテゴリ名> [JSONファイル]\n例: go run main.go 奇才\n例: go run main.go 奇才 test.json")
	}

	category := os.Args[1]
	jsonFile := config.DefaultJSONFile
	if len(os.Args) > 2 {
		jsonFile = os.Args[2]
	}

	return category, jsonFile
}

func showAvailableCategories(jsonFile string) {
	data, err := os.ReadFile(jsonFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%sファイルの読み込みに失敗しました: %v\n", jsonFile, err)
		return
	}

	var categorizedNames map[string][]string
	if err := json.Unmarshal(data, &categorizedNames); err != nil {
		fmt.Fprintf(os.Stderr, "JSONの解析に失敗しました: %v\n", err)
		return
	}

	showAvailableCategoriesWithData(categorizedNames)
}

func showAvailableCategoriesWithData(categorizedNames map[string][]string) {
	fmt.Fprintf(os.Stderr, "利用可能なカテゴリ:\n")
	for category, names := range categorizedNames {
		fmt.Fprintf(os.Stderr, "  %s (%d人)\n", category, len(names))
	}
	fmt.Fprintf(os.Stderr, "\n")
}

func processCategory(category, jsonFile string) []Character {
	urls, err := loadCharactersFromJSON(category, jsonFile)
	if err != nil {
		log.Fatal("キャラクターファイルの読み込みエラー:", err)
	}

	var characters []Character
	for i, url := range urls {
		fmt.Printf("処理中 (%d/%d): %s\n", i+1, len(urls), url)

		character, err := extractCharacterInfoWithRetry(url)
		if err != nil {
			handleProcessingError(url, err)
			continue
		}

		characters = append(characters, character)
		sleepBetweenRequests(i, len(urls))
	}

	return characters
}

func handleProcessingError(url string, err error) {
	procErr := &ProcessingError{
		URL:     url,
		Message: err.Error(),
		Err:     err,
	}

	if isRateLimitError(err) {
		log.Fatalf("レート制限に達しました。しばらく時間を置いてから再実行してください: %v", procErr)
	}
	log.Printf("%v", procErr)
}

func isRateLimitError(err error) bool {
	return containsAnyString(err.Error(), rules.RetryErrors) || strings.Contains(err.Error(), "最大リトライ回数に達しました")
}

func sleepBetweenRequests(currentIndex, totalCount int) {
	if currentIndex < totalCount-1 {
		time.Sleep(config.RequestDelay)
	}
}

func outputJSON(characters []Character) {
	// 没年昇順でソート
	sort.Slice(characters, func(i, j int) bool {
		return characters[i].DeathYear < characters[j].DeathYear
	})

	output, err := json.MarshalIndent(characters, "", "    ")
	if err != nil {
		log.Fatal("JSON変換エラー:", err)
	}

	jsonString := string(output)
	fmt.Println(jsonString)

	// クリップボードにコピー（macOSのみ）
	if runtime.GOOS == "darwin" {
		if err := copyToClipboard(jsonString); err != nil {
			log.Printf("クリップボードへのコピーに失敗しました: %v", err)
		} else {
			fmt.Fprintf(os.Stderr, "\n結果をクリップボードにコピーしました。\n")
		}
	}
}

func copyToClipboard(text string) error {
	cmd := exec.Command("pbcopy")
	cmd.Stdin = strings.NewReader(text)
	return cmd.Run()
}

func loadCharactersFromJSON(category, jsonFile string) ([]string, error) {
	data, err := os.ReadFile(jsonFile)
	if err != nil {
		return nil, fmt.Errorf("ファイル読み込みエラー: %v", err)
	}

	// カテゴリ別の武将名を格納するマップ
	var categorizedNames map[string][]string
	if err := json.Unmarshal(data, &categorizedNames); err != nil {
		return nil, fmt.Errorf("JSON解析エラー: %v", err)
	}

	// 指定されたカテゴリの武将名のみを使用
	selectedNames, exists := categorizedNames[category]
	if !exists {
		showAvailableCategoriesWithData(categorizedNames)
		return nil, fmt.Errorf("カテゴリ '%s' が見つかりません", category)
	}
	fmt.Printf("カテゴリ '%s' の武将を処理します (%d人)\n", category, len(selectedNames))

	// 武将名からURLを生成
	urls := make([]string, len(selectedNames))
	for i, name := range selectedNames {
		urls[i] = generateURL(name)
	}

	// 重複チェック
	if duplicates := findDuplicateURLs(urls); len(duplicates) > 0 {
		return nil, fmt.Errorf("重複するURLが見つかりました: %v", duplicates)
	}

	return urls, nil
}

func generateURL(name string) string {
	return config.BaseURL + url.QueryEscape(name)
}

func findDuplicateURLs(urls []string) []string {
	seen := make(map[string]int)
	var duplicates []string

	// 各URLの出現回数をカウント
	for i, url := range urls {
		if firstIndex, exists := seen[url]; exists {
			// 重複が見つかった場合、詳細情報を追加
			duplicateInfo := fmt.Sprintf("%s (位置: %d, %d)", url, firstIndex+1, i+1)
			if slices.Index(duplicates, duplicateInfo) == -1 {
				duplicates = append(duplicates, duplicateInfo)
			}
		} else {
			seen[url] = i
		}
	}

	return duplicates
}

func extractCharacterInfoWithRetry(url string) (Character, error) {
	for attempt := 0; attempt < config.MaxRetries; attempt++ {
		character, err := extractCharacterInfo(url)
		if err == nil {
			return character, nil
		}

		if shouldRetry(err, attempt, config.MaxRetries) {
			delay := config.BaseDelay * time.Duration(attempt+1)
			fmt.Printf("429エラーが発生しました。%v後にリトライします... (試行 %d/%d)\n", delay, attempt+2, config.MaxRetries)
			time.Sleep(delay)
			continue
		}

		return character, err
	}

	return Character{}, fmt.Errorf("最大リトライ回数に達しました")
}

func shouldRetry(err error, attempt, maxRetries int) bool {
	return containsAnyString(err.Error(), rules.RetryErrors) && attempt < maxRetries-1
}

func extractCharacterInfo(url string) (Character, error) {
	doc, err := fetchAndParseHTML(url)
	if err != nil {
		return Character{}, err
	}

	character := extractBasicInfo(doc)
	tactics, skills := extractTacticsAndSkills(doc)
	character.Tactics = tactics
	character.Skills = skills

	return character, nil
}

func fetchAndParseHTML(url string) (*html.Node, error) {
	client := &http.Client{Timeout: config.HTTPTimeout}

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return nil, fmt.Errorf("リクエスト作成エラー: %v", err)
	}

	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTPリクエストエラー: %v", err)
	}
	defer resp.Body.Close()

	if err := checkHTTPStatus(resp); err != nil {
		return nil, err
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("レスポンス読み込みエラー: %v", err)
	}

	doc, err := html.Parse(strings.NewReader(string(body)))
	if err != nil {
		return nil, fmt.Errorf("HTMLパースエラー: %v", err)
	}

	return doc, nil
}

func checkHTTPStatus(resp *http.Response) error {
	if resp.StatusCode == 429 {
		return fmt.Errorf("429 Too Many Requests")
	}
	if resp.StatusCode != 200 {
		return fmt.Errorf("HTTPエラー: %d %s", resp.StatusCode, resp.Status)
	}
	return nil
}

func extractBasicInfo(doc *html.Node) Character {
	character := Character{}

	extractNameAndReading(&character, doc)
	extractFromTables(&character, doc)
	extractInterests(&character, doc)

	return character
}

func extractNameAndReading(character *Character, doc *html.Node) {
	nameNode := findNodeWithText(doc, "strong")
	if nameNode == nil {
		return
	}

	text := getNodeText(nameNode)

	// strings.Cutを使って効率的に分割
	name, rest, found := strings.Cut(text, "(")
	if !found {
		return
	}

	reading, _, found := strings.Cut(rest, ")")
	if !found {
		return
	}

	character.Name = strings.TrimSpace(name)
	character.Reading = strings.TrimSpace(reading)
}

func extractFromTables(character *Character, doc *html.Node) {
	tables := findAllNodes(doc, "table")
	for _, table := range tables {
		switch {
		case containsAllTexts(table, rules.BasicInfoHeaders):
			extractBasicInfoFromTable(character, table)
		case containsAllTexts(table, rules.AbilityHeaders):
			extractAbilitiesFromTable(character, table)
			// 能力テーブルに奇才が含まれている場合もあるので、同じテーブルで奇才も抽出
			extractTalentFromTable(character, table)
		case containsAnyTexts(table, rules.TalentHeaders):
			extractTalentFromTable(character, table)
		}
	}

	// 奇才が見つからない場合、全テーブルから直接検索
	if character.Talent == "" {
		for _, table := range tables {
			extractTalentFromTable(character, table)
			if character.Talent != "" {
				break
			}
		}
	}
}

func extractBasicInfoFromTable(character *Character, table *html.Node) {
	rows := findAllNodes(table, "tr")
	for _, row := range rows {
		cells := findAllNodes(row, "td")
		if len(cells) < 9 {
			continue
		}

		// 字
		if len(cells) > 1 {
			character.Azana = strings.TrimSpace(getNodeText(cells[1]))
		}

		// 没年
		if len(cells) > 6 {
			if deathYear, err := strconv.Atoi(strings.TrimSpace(getNodeText(cells[6]))); err == nil {
				character.DeathYear = deathYear
				character.DeathMinus13 = deathYear - 13
			}
		}
		break
	}
}

func extractAbilitiesFromTable(character *Character, table *html.Node) {
	rows := findAllNodes(table, "tr")
	for _, row := range rows {
		cells := findAllNodes(row, "td")
		processTableRow(character, row, cells)
	}
}

func processTableRow(character *Character, row *html.Node, cells []*html.Node) {
	extractAbilities(character, cells)
	extractPersonalityAndLoyalty(character, cells)
	extractStatusInfo(character, row, cells)
}

func extractAbilities(character *Character, cells []*html.Node) {
	if len(cells) < 5 {
		return
	}

	abilities := make([]int, 5)
	allNumbers := true

	for i := 0; i < 5 && i < len(cells); i++ {
		text := strings.TrimSpace(getNodeText(cells[i]))
		if val, err := strconv.Atoi(text); err == nil {
			abilities[i] = val
		} else {
			allNumbers = false
			break
		}
	}

	if allNumbers && abilities[0] > 0 {
		character.Leadership = abilities[0]
		character.Force = abilities[1]
		character.Intelligence = abilities[2]
		character.Politics = abilities[3]
		character.Charm = abilities[4]
	}
}

func extractPersonalityAndLoyalty(character *Character, cells []*html.Node) {
	if len(cells) < 2 {
		return
	}

	// 現在の行で性格を探す
	for j, cell := range cells {
		text := strings.TrimSpace(getNodeText(cell))
		if !slices.Contains(rules.PersonalityTypes, text) {
			continue
		}

		character.Personality = text

		// 義理を探す
		for k := j + 1; k < len(cells); k++ {
			loyaltyText := strings.TrimSpace(getNodeText(cells[k]))
			if val, err := strconv.Atoi(loyaltyText); err == nil {
				character.Loyalty = val
				break
			}
		}
		return
	}
}

func extractStatusInfo(character *Character, row *html.Node, cells []*html.Node) {
	// ヘッダー行をスキップ
	if containsAnyTexts(row, rules.StatusHeaders) {
		return
	}

	if len(cells) < 3 {
		return
	}

	for j, cell := range cells {
		text := strings.TrimSpace(getNodeText(cell))
		if !slices.Contains(rules.FameTypes, text) {
			continue
		}

		character.Fame = text
		extractGreed(character, cells, j)
		extractStrategy(character, cells, j)
		break
	}
}

func extractGreed(character *Character, cells []*html.Node, startIndex int) {
	if startIndex+1 >= len(cells) {
		return
	}

	greedText := strings.TrimSpace(getNodeText(cells[startIndex+1]))
	if greedText != "" && greedText != "-" && greedText != "ー" {
		character.Greed = greedText
	}
}

func extractStrategy(character *Character, cells []*html.Node, startIndex int) {
	for k := startIndex + 2; k < len(cells) && k < startIndex+4; k++ {
		strategyText := strings.TrimSpace(getNodeText(cells[k]))
		if strategyText == "" || strategyText == "ー" {
			continue
		}

		if slices.Contains(rules.StrategyTypes, strategyText) {
			character.Strategy = strategyText
			break
		} else if strategyText == "-" {
			character.Strategy = "-"
			break
		}
	}
}

func extractTalentFromTable(character *Character, table *html.Node) {
	// 奇才テーブルかどうかを確認（「奇才」「効果」のヘッダーを持つ）
	if !isTalentTable(table) {
		return
	}

	rows := findAllNodes(table, "tr")
	for _, row := range rows {
		cells := findAllNodes(row, "td")
		for _, cell := range cells {
			if hasStyle(cell, "background-color:gold") {
				character.Talent = strings.TrimSpace(getNodeText(cell))
				return
			}
		}
	}
}

func isTalentTable(table *html.Node) bool {
	// テーブル全体のテキストを確認
	tableText := getNodeText(table)

	// 「奇才」と「効果」の両方が含まれている場合のみ奇才テーブルとみなす
	return strings.Contains(tableText, "奇才") && strings.Contains(tableText, "効果")
}

func extractInterests(character *Character, doc *html.Node) {
	var interests []string
	allCells := findAllNodes(doc, "td")

	for _, cell := range allCells {
		if !hasAnyStyleWidth(cell, rules.InterestWidths) {
			continue
		}

		text := strings.TrimSpace(getNodeText(cell))
		if slices.Contains(rules.ExcludeTexts, text) || !isInterestCell(text) {
			continue
		}

		interests = append(interests, text)
	}

	character.Interest = strings.Join(interests, ", ")
}

func extractTacticsAndSkills(doc *html.Node) (string, string) {
	var tactics, skills []string

	tables := findAllNodes(doc, "table")
	for _, table := range tables {
		switch {
		case containsAnyTexts(table, rules.TacticsHeaders):
			tactics = extractFromSkillTable(table, isTacticCategory)
		case containsAnyTexts(table, rules.SkillsHeaders):
			skills = extractFromSkillTable(table, isSkillCategory)
		}
	}

	return strings.Join(tactics, ", "), strings.Join(skills, ", ")
}

func extractFromSkillTable(table *html.Node, isCategory func(string) bool) []string {
	var items []string

	rows := findAllNodes(table, "tr")
	for _, row := range rows {
		cells := findAllNodes(row, "td")
		for _, cell := range cells {
			if !hasStyleWidth(cell, "70px") {
				continue
			}

			text := cleanTacticSkillText(strings.TrimSpace(getNodeText(cell)))
			if text != "" && !isCategory(text) {
				items = append(items, text)
			}
		}
	}

	return items
}

// ========================================
// HTML操作ヘルパー関数
// ========================================

func findAllNodes(n *html.Node, tagName string) []*html.Node {
	var nodes []*html.Node
	var traverse func(*html.Node)

	traverse = func(node *html.Node) {
		if node.Type == html.ElementNode && node.Data == tagName {
			nodes = append(nodes, node)
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			traverse(child)
		}
	}

	traverse(n)
	return nodes
}

func findNodeWithText(n *html.Node, tagName string) *html.Node {
	var result *html.Node
	var traverse func(*html.Node)

	traverse = func(node *html.Node) {
		if result != nil {
			return
		}
		if node.Type == html.ElementNode && node.Data == tagName {
			result = node
			return
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			traverse(child)
		}
	}

	traverse(n)
	return result
}

func getNodeText(n *html.Node) string {
	var text strings.Builder
	var traverse func(*html.Node)

	traverse = func(node *html.Node) {
		if node.Type == html.TextNode {
			text.WriteString(node.Data)
		}
		for child := node.FirstChild; child != nil; child = child.NextSibling {
			traverse(child)
		}
	}

	traverse(n)
	return text.String()
}

func hasStyle(n *html.Node, style string) bool {
	for _, attr := range n.Attr {
		if attr.Key == "style" && strings.Contains(attr.Val, style) {
			return true
		}
	}
	return false
}

func hasStyleWidth(n *html.Node, width string) bool {
	for _, attr := range n.Attr {
		if attr.Key == "style" && strings.Contains(attr.Val, "width:"+width) {
			return true
		}
	}
	return false
}

func hasAnyStyleWidth(n *html.Node, widths []string) bool {
	for _, attr := range n.Attr {
		if attr.Key == "style" {
			for _, width := range widths {
				if strings.Contains(attr.Val, "width:"+width) {
					return true
				}
			}
		}
	}
	return false
}

func isTacticCategory(text string) bool {
	return slices.Contains(rules.TacticCategories, text)
}

func isSkillCategory(text string) bool {
	return slices.Contains(rules.SkillCategories, text)
}

func isInterestCell(text string) bool {
	return slices.Contains(rules.InterestItems, text)
}

func cleanTacticSkillText(text string) string {
	// strings.Cutを使って効率的に括弧を除去
	before, _, _ := strings.Cut(text, "(")
	return strings.TrimSpace(before)
}

func containsAnyString(text string, substrings []string) bool {
	for _, substring := range substrings {
		if strings.Contains(text, substring) {
			return true
		}
	}
	return false
}

func containsAllTexts(n *html.Node, texts []string) bool {
	nodeText := getNodeText(n)
	for _, text := range texts {
		if !strings.Contains(nodeText, text) {
			return false
		}
	}
	return true
}

func containsAnyTexts(n *html.Node, texts []string) bool {
	nodeText := getNodeText(n)
	for _, text := range texts {
		if strings.Contains(nodeText, text) {
			return true
		}
	}
	return false
}
