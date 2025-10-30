package storage

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"autobot/internal/config"
	loggerpkg "autobot/internal/logger"
)

const (
	decisionsFileName = "decisions.jsonl"
	tradesFileName    = "trades.jsonl"
	recentLimit       = 200
)

type fileStore struct {
	cfg          config.StorageConfig
	decFile      *os.File
	tradeFile    *os.File
	mu           sync.Mutex
	decisionsBuf []DecisionRecord
	tradesBuf    []TradeRecord
	logger       *loggerpkg.ModuleLogger
}

func newFileStore(cfg config.StorageConfig) (Store, error) {
	if cfg.Path == "" {
		cfg.Path = "data"
	}
	if err := os.MkdirAll(cfg.Path, 0o755); err != nil {
		return nil, fmt.Errorf("create storage path: %w", err)
	}

	decPath := filepath.Join(cfg.Path, decisionsFileName)
	decFile, err := os.OpenFile(decPath, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open decisions file: %w", err)
	}

	tradePath := filepath.Join(cfg.Path, tradesFileName)
	tradeFile, err := os.OpenFile(tradePath, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0o644)
	if err != nil {
		decFile.Close()
		return nil, fmt.Errorf("open trades file: %w", err)
	}

	logger := loggerpkg.Get("storage")
	store := &fileStore{
		cfg:       cfg,
		decFile:   decFile,
		tradeFile: tradeFile,
		logger:    logger,
	}

	store.decisionsBuf = store.loadRecentDecisions(decPath)
	store.tradesBuf = store.loadRecentTrades(tradePath)
	if logger != nil {
		logger.Printf("file store ready path=%s", cfg.Path)
	}

	return store, nil
}

func (s *fileStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var err error
	if s.decFile != nil {
		if e := s.decFile.Close(); e != nil {
			err = e
		}
	}
	if s.tradeFile != nil {
		if e := s.tradeFile.Close(); e != nil {
			err = e
		}
	}
	if s.logger != nil {
		s.logger.Printf("store closed err=%v", err)
	}
	return err
}

func (s *fileStore) RecordDecision(ctx context.Context, record DecisionRecord) error {
	record.CreatedAt = time.Now().UnixMilli()
	payload, err := json.Marshal(record)
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, err := s.decFile.Write(append(payload, '\n')); err != nil {
		return err
	}
	s.decisionsBuf = append(s.decisionsBuf, record)
	if len(s.decisionsBuf) > recentLimit {
		s.decisionsBuf = s.decisionsBuf[len(s.decisionsBuf)-recentLimit:]
	}
	if s.logger != nil {
		s.logger.Printf("decision recorded trader=%s action=%s confidence=%.2f", record.Trader, record.Action, record.Confidence)
	}
	return nil
}

func (s *fileStore) RecordTrade(ctx context.Context, record TradeRecord) error {
	record.CreatedAt = time.Now().UnixMilli()
	payload, err := json.Marshal(record)
	if err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	if _, err := s.tradeFile.Write(append(payload, '\n')); err != nil {
		return err
	}
	s.tradesBuf = append(s.tradesBuf, record)
	if len(s.tradesBuf) > recentLimit {
		s.tradesBuf = s.tradesBuf[len(s.tradesBuf)-recentLimit:]
	}
	if s.logger != nil {
		s.logger.Printf("trade recorded trader=%s action=%s qty=%.4f price=%.2f pnl=%.4f", record.Trader, record.Action, record.Quantity, record.Price, record.PnL)
	}
	return nil
}

func (s *fileStore) RecentDecisions(ctx context.Context, limit int) ([]DecisionRecord, error) {
	if limit <= 0 || limit > len(s.decisionsBuf) {
		limit = len(s.decisionsBuf)
	}
	result := make([]DecisionRecord, limit)
	copy(result, s.decisionsBuf[len(s.decisionsBuf)-limit:])
	return result, nil
}

func (s *fileStore) RecentTrades(ctx context.Context, limit int) ([]TradeRecord, error) {
	if limit <= 0 || limit > len(s.tradesBuf) {
		limit = len(s.tradesBuf)
	}
	result := make([]TradeRecord, limit)
	copy(result, s.tradesBuf[len(s.tradesBuf)-limit:])
	return result, nil
}

func (s *fileStore) loadRecentDecisions(path string) []DecisionRecord {
	file, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	records := make([]DecisionRecord, 0, recentLimit)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var rec DecisionRecord
		if err := json.Unmarshal(line, &rec); err != nil {
			continue
		}
		records = append(records, rec)
		if len(records) > recentLimit {
			records = records[1:]
		}
	}
	return records
}

func (s *fileStore) loadRecentTrades(path string) []TradeRecord {
	file, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	records := make([]TradeRecord, 0, recentLimit)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var rec TradeRecord
		if err := json.Unmarshal(line, &rec); err != nil {
			continue
		}
		records = append(records, rec)
		if len(records) > recentLimit {
			records = records[1:]
		}
	}
	return records
}
