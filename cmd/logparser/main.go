package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

var (
	moduleFlag   = flag.String("module", "", "仅解析指定模块 (对应日志文件名去掉扩展名)“")
	dirFlag      = flag.String("dir", "logs", "日志目录")
	outputFlag   = flag.String("out", "", "输出文件路径，留空则输出到标准输出")
	includeFiles = flag.Bool("include-file", true, "是否在输出中包含文件路径与行号")
)

var linePattern = regexp.MustCompile(`^\[([^\]]+)\]\s+(\d{4}/\d{2}/\d{2}\s+\d{2}:\d{2}:\d{2}(?:\.\d{6})?)\s*(.*)$`)
var kvPattern = regexp.MustCompile(`([a-zA-Z0-9_]+)=([^\s]+)`)

func main() {
	flag.Parse()

	entries, err := parseLogs(*dirFlag, *moduleFlag)
	if err != nil {
		fmt.Fprintf(os.Stderr, "parse logs: %v\n", err)
		os.Exit(1)
	}

	var writer io.Writer = os.Stdout
	if *outputFlag != "" {
		f, err := os.Create(*outputFlag)
		if err != nil {
			fmt.Fprintf(os.Stderr, "create output: %v\n", err)
			os.Exit(1)
		}
		defer f.Close()
		writer = f
	}

	enc := json.NewEncoder(writer)
	enc.SetEscapeHTML(false)
	for _, entry := range entries {
		if err := enc.Encode(entry); err != nil {
			fmt.Fprintf(os.Stderr, "encode entry: %v\n", err)
			os.Exit(1)
		}
	}
}

// Record 表示一条结构化日志。
type Record struct {
	Timestamp time.Time         `json:"timestamp"`
	Module    string            `json:"module"`
	Message   string            `json:"message"`
	Fields    map[string]string `json:"fields,omitempty"`
	File      string            `json:"file,omitempty"`
	Line      int               `json:"line,omitempty"`
}

func parseLogs(dir, module string) ([]Record, error) {
	entries := make([]Record, 0)

	items, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	sort.Slice(items, func(i, j int) bool { return items[i].Name() < items[j].Name() })

	for _, item := range items {
		if item.IsDir() {
			continue
		}
		if !strings.HasSuffix(item.Name(), ".log") {
			continue
		}
		modName := strings.TrimSuffix(item.Name(), ".log")
		if module != "" && module != modName {
			continue
		}
		filePath := filepath.Join(dir, item.Name())
		fileEntries, err := parseFile(filePath, modName)
		if err != nil {
			return nil, fmt.Errorf("parse %s: %w", filePath, err)
		}
		entries = append(entries, fileEntries...)
	}

	sort.Slice(entries, func(i, j int) bool {
		if entries[i].Timestamp.Equal(entries[j].Timestamp) {
			return entries[i].Module < entries[j].Module
		}
		return entries[i].Timestamp.Before(entries[j].Timestamp)
	})

	if !*includeFiles {
		for i := range entries {
			entries[i].File = ""
			entries[i].Line = 0
		}
	}

	return entries, nil
}

func parseFile(path, fallbackModule string) ([]Record, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	records := make([]Record, 0)
	scanner := bufio.NewScanner(file)
	lineNum := 0

	for scanner.Scan() {
		lineNum++
		line := scanner.Text()
		if strings.TrimSpace(line) == "" {
			continue
		}
		match := linePattern.FindStringSubmatch(line)
		if match == nil {
			records = append(records, Record{
				Timestamp: time.Time{},
				Module:    fallbackModule,
				Message:   line,
				File:      path,
				Line:      lineNum,
			})
			continue
		}

		module := match[1]
		tsStr := match[2]
		message := match[3]
		if module == "" {
			module = fallbackModule
		}

		ts, err := time.ParseInLocation("2006/01/02 15:04:05.000000", tsStr, time.Local)
		if err != nil {
			ts = time.Time{}
		}

		fields := extractFields(message)

		records = append(records, Record{
			Timestamp: ts,
			Module:    module,
			Message:   message,
			Fields:    fields,
			File:      path,
			Line:      lineNum,
		})
	}

	if err := scanner.Err(); err != nil {
		return nil, err
	}

	return records, nil
}

func extractFields(message string) map[string]string {
	matches := kvPattern.FindAllStringSubmatch(message, -1)
	if len(matches) == 0 {
		return nil
	}
	fields := make(map[string]string, len(matches))
	for _, m := range matches {
		if len(m) < 3 {
			continue
		}
		fields[m[1]] = m[2]
	}
	if len(fields) == 0 {
		return nil
	}
	return fields
}
