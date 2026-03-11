package atsf4g_go_robot_util

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

const (
	historyFilePath = "./cmd_history.tmp"
	maxHistory      = 30 // 最大历史条数
)

type HistoryManager struct {
	File                string
	Items               []string
	Set                 map[string]struct{}
	needCleanDuplicates bool
}

func NewHistoryManager(file string, cleanDuplicates bool) *HistoryManager {
	h := &HistoryManager{
		File:                file,
		Set:                 make(map[string]struct{}),
		needCleanDuplicates: cleanDuplicates,
	}
	h.load()
	if h.needCleanDuplicates {
		h.cleanDuplicates() // ✅ 启动时自动清理重复记录
	}
	h.trimToMax() // ✅ 启动时控制最大长度
	h.save()      // ✅ 清理后立即保存
	return h
}

// 加载历史到内存
func (h *HistoryManager) load() {
	f, err := os.Open(h.File)
	if err != nil {
		return
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" {
			h.Items = append(h.Items, line)
			h.Set[line] = struct{}{}
		}
	}
}

// 清理重复（保留最新）
func (h *HistoryManager) cleanDuplicates() {
	seen := make(map[string]struct{})
	newItems := make([]string, 0, len(h.Items))
	for i := len(h.Items) - 1; i >= 0; i-- { // 倒序，保留最新
		line := h.Items[i]
		if _, ok := seen[line]; !ok {
			seen[line] = struct{}{}
			newItems = append(newItems, line)
		}
	}
	// 反转回正常顺序
	for i, j := 0, len(newItems)-1; i < j; i, j = i+1, j-1 {
		newItems[i], newItems[j] = newItems[j], newItems[i]
	}
	h.Items = newItems
	h.Set = seen
}

// 限制最大历史长度
func (h *HistoryManager) trimToMax() {
	if len(h.Items) > maxHistory {
		h.Items = h.Items[len(h.Items)-maxHistory:]
		h.Set = make(map[string]struct{}, len(h.Items))
		for _, item := range h.Items {
			h.Set[item] = struct{}{}
		}
	}
}

// 保存历史到文件
func (h *HistoryManager) save() {
	f, err := os.Create(h.File)
	if err != nil {
		fmt.Println("save history failed:", err)
		return
	}
	defer f.Close()

	writer := bufio.NewWriter(f)
	for _, cmd := range h.Items {
		fmt.Fprintln(writer, cmd)
	}
	writer.Flush()
}

// 添加命令（去重 + 保留最新 + 限制最大长度）
func (h *HistoryManager) add(line string) {
	line = strings.TrimSpace(line)
	if line == "" {
		return
	}

	if h.needCleanDuplicates {
		// 删除旧的重复
		newItems := make([]string, 0, len(h.Items))
		for _, item := range h.Items {
			if item != line {
				newItems = append(newItems, item)
			}
		}
		h.Items = append(newItems, line)
	} else {
		h.Items = append(h.Items, line)
	}
	h.trimToMax()

	if h.needCleanDuplicates {
		h.Set = make(map[string]struct{}, len(h.Items))
		for _, item := range h.Items {
			h.Set[item] = struct{}{}
		}
	}

	h.save()
}
