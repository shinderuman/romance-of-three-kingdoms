package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"golang.org/x/net/html"
)

// ========================================
// 定数定義
// ========================================

var (
	// カテゴリ分類
	tacticCategories = []string{"歩兵", "騎兵", "弓兵", "艦船", "軍略", "補助", "遁甲"}
	skillCategories  = []string{"任務", "智謀", "兵科", "軍事"}
	interestItems    = []string{"武具", "書物", "宝物", "茶器", "名馬", "美術", "酒", "音楽", "詩歌", "絵画", "香", "薬草"}

	// 武将属性
	personalityTypes = []string{"豪胆", "冷静", "剛胆", "沈着", "猪突"}
	fameTypes        = []string{"無関心", "重視", "文武不問", "武名", "高名"}
	strategyTypes    = []string{"好戦", "普通", "積極", "消極", "私欲"}

	// HTML解析用
	interestWidths = []string{"60px", "53px", "52px", "51px", "50px"}
	excludeTexts   = []string{"ー", "", "興味", "-"}

	// エラー処理
	retryErrors = []string{"429", "Too Many Requests"}

	// テーブル識別用ヘッダー
	basicInfoHeaders = []string{"字", "没年"}
	abilityHeaders   = []string{"統率", "武力"}
	statusHeaders    = []string{"重視名声", "物欲", "戦略傾向"}
	talentHeaders    = []string{"奇才"}
	tacticsHeaders   = []string{"戦法"}
	skillsHeaders    = []string{"特技"}
)

// ========================================
// 構造体定義
// ========================================

// Character 武将の情報を格納する構造体
type Character struct {
	Name         string `json:"名前"`
	Reading      string `json:"読み"`
	Zi           string `json:"字"`
	DeathYear    int    `json:"没年"`
	DeathPlus1   int    `json:"没年+1"`
	Leadership   int    `json:"統率"`
	Force        int    `json:"武力"`
	Intelligence int    `json:"知力"`
	Politics     int    `json:"政治"`
	Charm        int    `json:"魅力"`
	Personality  string `json:"性格"`
	Loyalty      int    `json:"義理"`
	Fame         string `json:"重視名声"`
	Greed        string `json:"物欲"`
	Strategy     string `json:"戦略傾向"`
	Interest     string `json:"興味"`
	Talent       string `json:"奇才"`
	Tactics      string `json:"戦法"`
	Skills       string `json:"特技"`
}

// ========================================
// メイン処理
// ========================================

func main() {
	configFile := getConfigFile()
	urls := loadURLs(configFile)
	characters := processAllCharacters(urls)
	outputJSON(characters)
}

func getConfigFile() string {
	if len(os.Args) > 1 {
		return os.Args[1]
	}
	return "urls.json"
}

func loadURLs(filename string) []string {
	urls, err := loadURLsFromJSON(filename)
	if err != nil {
		log.Fatal("URLの読み込みエラー:", err)
	}
	return urls
}

func processAllCharacters(urls []string) []Character {
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
	if isRateLimitError(err) {
		log.Fatalf("レート制限に達しました。しばらく時間を置いてから再実行してください: %v", err)
	}
	log.Printf("URL %s の処理中にエラーが発生しました: %v", url, err)
}

func isRateLimitError(err error) bool {
	errMsg := err.Error()
	return containsAnyString(errMsg, retryErrors) || strings.Contains(errMsg, "最大リトライ回数に達しました")
}

func sleepBetweenRequests(currentIndex, totalCount int) {
	if currentIndex < totalCount-1 {
		time.Sleep(500 * time.Millisecond)
	}
}

func outputJSON(characters []Character) {
	output, err := json.MarshalIndent(characters, "", "    ")
	if err != nil {
		log.Fatal("JSON変換エラー:", err)
	}
	fmt.Println(string(output))
}

// ========================================
// ファイル読み込み
// ========================================

func loadURLsFromJSON(filename string) ([]string, error) {
	data, err := readFile(filename)
	if err != nil {
		return nil, err
	}

	var urls []string
	if err := json.Unmarshal(data, &urls); err != nil {
		return nil, fmt.Errorf("JSON解析エラー: %v", err)
	}

	return urls, nil
}

func readFile(filename string) ([]byte, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, fmt.Errorf("ファイル読み込みエラー: %v", err)
	}
	defer file.Close()

	data, err := io.ReadAll(file)
	if err != nil {
		return nil, fmt.Errorf("ファイル内容読み込みエラー: %v", err)
	}

	return data, nil
}

// ========================================
// HTTP処理とリトライ
// ========================================

func extractCharacterInfoWithRetry(url string) (Character, error) {
	const maxRetries = 3
	const baseDelay = 2 * time.Second

	for attempt := 0; attempt < maxRetries; attempt++ {
		character, err := extractCharacterInfo(url)
		if err == nil {
			return character, nil
		}

		if shouldRetry(err, attempt, maxRetries) {
			delay := baseDelay * time.Duration(attempt+1)
			fmt.Printf("429エラーが発生しました。%v後にリトライします... (試行 %d/%d)\n", delay, attempt+2, maxRetries)
			time.Sleep(delay)
			continue
		}

		return character, err
	}

	return Character{}, fmt.Errorf("最大リトライ回数に達しました")
}

func shouldRetry(err error, attempt, maxRetries int) bool {
	return containsAnyString(err.Error(), retryErrors) && attempt < maxRetries-1
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
	client := &http.Client{Timeout: 30 * time.Second}

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

// ========================================
// 基本情報抽出
// ========================================

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
	if !strings.Contains(text, "(") || !strings.Contains(text, ")") {
		return
	}

	parts := strings.Split(text, "(")
	if len(parts) < 2 {
		return
	}

	character.Name = strings.TrimSpace(parts[0])
	reading := strings.Split(parts[1], ")")[0]
	character.Reading = strings.TrimSpace(reading)
}

func extractFromTables(character *Character, doc *html.Node) {
	tables := findAllNodes(doc, "table")
	for _, table := range tables {
		switch {
		case containsAllTexts(table, basicInfoHeaders):
			extractBasicInfoFromTable(character, table)
		case containsAllTexts(table, abilityHeaders):
			extractAbilitiesFromTable(character, table)
		case containsAnyTexts(table, talentHeaders):
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
			character.Zi = strings.TrimSpace(getNodeText(cells[1]))
		}

		// 没年
		if len(cells) > 6 {
			if deathYear, err := strconv.Atoi(strings.TrimSpace(getNodeText(cells[6]))); err == nil {
				character.DeathYear = deathYear
				character.DeathPlus1 = deathYear + 1
			}
		}
		break
	}
}

func extractAbilitiesFromTable(character *Character, table *html.Node) {
	rows := findAllNodes(table, "tr")
	for _, row := range rows {
		cells := findAllNodes(row, "td")

		extractAbilities(character, cells)
		extractPersonalityAndLoyalty(character, cells)
		extractStatusInfo(character, row, cells)
	}
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

	for j, cell := range cells {
		text := strings.TrimSpace(getNodeText(cell))
		if !slices.Contains(personalityTypes, text) {
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
		break
	}
}

func extractStatusInfo(character *Character, row *html.Node, cells []*html.Node) {
	// ヘッダー行をスキップ
	if containsAnyTexts(row, statusHeaders) {
		return
	}

	if len(cells) < 3 {
		return
	}

	for j, cell := range cells {
		text := strings.TrimSpace(getNodeText(cell))
		if !slices.Contains(fameTypes, text) {
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

		if slices.Contains(strategyTypes, strategyText) {
			character.Strategy = strategyText
			break
		} else if strategyText == "-" {
			character.Strategy = "-"
			break
		}
	}
}

func extractTalentFromTable(character *Character, table *html.Node) {
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

func extractInterests(character *Character, doc *html.Node) {
	var interests []string
	allCells := findAllNodes(doc, "td")

	for _, cell := range allCells {
		if !hasAnyStyleWidth(cell, interestWidths) {
			continue
		}

		text := strings.TrimSpace(getNodeText(cell))
		if slices.Contains(excludeTexts, text) || !isInterestCell(text) {
			continue
		}

		interests = append(interests, text)
	}

	character.Interest = strings.Join(interests, ", ")
}

// ========================================
// 戦法・特技抽出
// ========================================

func extractTacticsAndSkills(doc *html.Node) (string, string) {
	var tactics, skills []string

	tables := findAllNodes(doc, "table")
	for _, table := range tables {
		switch {
		case containsAnyTexts(table, tacticsHeaders):
			tactics = extractFromSkillTable(table, isTacticCategory)
		case containsAnyTexts(table, skillsHeaders):
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

// ========================================
// スタイル・属性チェック関数
// ========================================

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

// ========================================
// 分類・判定関数
// ========================================

func isTacticCategory(text string) bool {
	return slices.Contains(tacticCategories, text)
}

func isSkillCategory(text string) bool {
	return slices.Contains(skillCategories, text)
}

func isInterestCell(text string) bool {
	return slices.Contains(interestItems, text)
}

// ========================================
// テキスト処理ヘルパー関数
// ========================================

func cleanTacticSkillText(text string) string {
	if idx := strings.Index(text, "("); idx != -1 {
		text = text[:idx]
	}
	return strings.TrimSpace(text)
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
