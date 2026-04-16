package dbtool

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	base "github.com/atframework/robot-go/base"
	utils "github.com/atframework/robot-go/utils"
)

// Shell 提供 dbtool 的交互式命令行界面
type Shell struct {
	querier  *Querier
	registry *Registry
}

// NewShell 创建交互式 Shell
func NewShell(querier *Querier, registry *Registry) *Shell {
	return &Shell{
		querier:  querier,
		registry: registry,
	}
}

// Run 启动交互式 readline 循环
func (s *Shell) Run() error {
	root := utils.CreateCommandNode()

	// 注册内置命令
	utils.RegisterCommandDefaultTimeout(root, []string{"tables"},
		func(_ base.TaskActionImpl, _ []string) string {
			return s.tablesString()
		}, "", "Show all available tables", nil)

	// 动态注册 table + index 命令
	tableNames := s.registry.GetTableNames()
	for _, name := range tableNames {
		info := s.registry.FindTableByShortName(name)
		if info == nil {
			continue
		}

		tableName := name
		tableInfo := info

		// table 级别：显示可用 indexes
		utils.RegisterCommandDefaultTimeout(root, []string{tableName},
			func(_ base.TaskActionImpl, _ []string) string {
				return s.tableIndexesString(tableName, tableInfo)
			}, "", fmt.Sprintf("Table %s", tableName), nil)

		// index 级别：执行查询
		for i := range info.Indexes {
			idx := &info.Indexes[i]
			utils.RegisterCommand(root, []string{tableName, idx.Name},
				s.makeQueryHandler(tableInfo, idx),
				s.buildArgsInfo(idx),
				fmt.Sprintf("Query %s (%s)", idx.Name, idx.Type),
				nil,
				30*time.Second)

			// 为 SORTED_SET 类型添加子命令补全提示
			if idx.Type == IndexTypeSortedSet {
				idxNode := root.Children[strings.ToLower(tableName)].Children[strings.ToLower(idx.Name)]
				for _, sub := range []string{"count", "rank", "rrank", "score", "rscore"} {
					idxNode.Children[strings.ToLower(sub)] = &utils.CommandNode{
						Children: make(map[string]*utils.CommandNode),
						Name:     sub,
						FullName: idxNode.FullName + " " + sub,
					}
				}
			}
		}
	}

	s.printWelcome()
	utils.ReadLine(root)
	return nil
}

// printWelcome 打印欢迎信息和可用表列表
func (s *Shell) printWelcome() {
	fmt.Println("=== DBTOOL - Database Table Inspector ===")
	fmt.Printf("Loaded %d table(s) with database_table option\n", len(s.registry.tables))
	fmt.Printf("Record prefix: %s\n", s.querier.recordPrefix)
	fmt.Println()
	fmt.Print(s.tablesString())
	fmt.Println()
}

// tablesString 返回所有可用的表信息字符串
func (s *Shell) tablesString() string {
	tables := s.registry.GetAllTables()
	if len(tables) == 0 {
		return "No tables found with database_table option"
	}

	var sb strings.Builder
	sb.WriteString("Available tables:\n")
	for _, t := range tables {
		for _, idx := range t.Indexes {
			keyInfo := strings.Join(idx.KeyFields, ", ")
			sb.WriteString(fmt.Sprintf("  %-40s  index: %-25s  type: %-10s  keys: [%s]\n",
				t.MessageDesc.Name(), idx.Name, idx.Type, keyInfo))
		}
	}
	return sb.String()
}

// tableIndexesString 返回指定表的索引信息字符串
func (s *Shell) tableIndexesString(tableName string, info *TableInfo) string {
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("Table: %s\n", tableName))
	sb.WriteString("Available indexes:\n")
	for _, idx := range info.Indexes {
		keyInfo := strings.Join(idx.KeyFields, ", ")
		sb.WriteString(fmt.Sprintf("  %-25s  type: %-10s  keys: [%s]\n", idx.Name, idx.Type, keyInfo))
	}
	return sb.String()
}

// buildArgsInfo 构建命令参数描述
func (s *Shell) buildArgsInfo(idx *TableIndex) string {
	parts := make([]string, 0, len(idx.KeyFields)+1)
	for _, kf := range idx.KeyFields {
		parts = append(parts, "<"+kf+">")
	}
	switch idx.Type {
	case IndexTypeKL:
		parts = append(parts, "[listIdx]")
	case IndexTypeSortedSet:
		parts = append(parts, "<subcommand> ...")
	}
	return strings.Join(parts, " ")
}

// makeQueryHandler 创建查询命令处理函数
func (s *Shell) makeQueryHandler(info *TableInfo, index *TableIndex) utils.CommandFunc {
	return func(_ base.TaskActionImpl, args []string) string {
		if len(args) < len(index.KeyFields) {
			return fmt.Sprintf("Index '%s' requires %d key field(s): [%s]\nProvided: %d",
				index.Name, len(index.KeyFields), strings.Join(index.KeyFields, ", "), len(args))
		}

		keyValues := args[:len(index.KeyFields)]
		extraArgs := args[len(index.KeyFields):]

		switch index.Type {
		case IndexTypeKV:
			return s.executeKV(info, index, keyValues)
		case IndexTypeKL:
			return s.executeKL(info, index, keyValues, extraArgs)
		case IndexTypeSortedSet:
			return s.executeSortedSet(index, keyValues, extraArgs)
		default:
			return fmt.Sprintf("Unsupported index type: %s", index.Type)
		}
	}
}

func (s *Shell) executeKV(info *TableInfo, index *TableIndex, keyValues []string) string {
	result, err := s.querier.QueryKV(info, index, keyValues)
	if err != nil {
		return fmt.Sprintf("Error: %v", err)
	}
	return result
}

func (s *Shell) executeKL(info *TableInfo, index *TableIndex, keyValues []string, extraArgs []string) string {
	listIndex := int64(-1) // -1 means all
	if len(extraArgs) > 0 {
		idx, err := strconv.ParseInt(extraArgs[0], 10, 64)
		if err != nil {
			return fmt.Sprintf("Invalid list index: %s", extraArgs[0])
		}
		listIndex = idx
	}

	result, err := s.querier.QueryKL(info, index, keyValues, listIndex)
	if err != nil {
		return fmt.Sprintf("Error: %v", err)
	}
	return result
}

func (s *Shell) executeSortedSet(index *TableIndex, keyValues []string, extraArgs []string) string {
	if len(extraArgs) == 0 {
		result, err := s.querier.QuerySortedSetCount(index, keyValues)
		if err != nil {
			return fmt.Sprintf("Error: %v", err)
		}
		return result + "\nSubcommands: count, rank <start> <stop>, rrank <start> <stop>, score <min> <max> [offset] [count], rscore <min> <max> [offset] [count]"
	}

	subCmd := strings.ToLower(extraArgs[0])
	switch subCmd {
	case "count":
		result, err := s.querier.QuerySortedSetCount(index, keyValues)
		if err != nil {
			return fmt.Sprintf("Error: %v", err)
		}
		return result

	case "rank", "rrank":
		if len(extraArgs) < 3 {
			return fmt.Sprintf("Usage: ... %s <start> <stop>", subCmd)
		}
		start, err := strconv.ParseInt(extraArgs[1], 10, 64)
		if err != nil {
			return fmt.Sprintf("Invalid start: %s", extraArgs[1])
		}
		stop, err := strconv.ParseInt(extraArgs[2], 10, 64)
		if err != nil {
			return fmt.Sprintf("Invalid stop: %s", extraArgs[2])
		}
		result, err := s.querier.QuerySortedSetByRank(index, keyValues, start, stop, subCmd == "rrank")
		if err != nil {
			return fmt.Sprintf("Error: %v", err)
		}
		return result

	case "score", "rscore":
		if len(extraArgs) < 3 {
			return fmt.Sprintf("Usage: ... %s <min> <max> [offset] [count]", subCmd)
		}
		min := extraArgs[1]
		max := extraArgs[2]
		var offset, count int64
		if len(extraArgs) > 3 {
			offset, _ = strconv.ParseInt(extraArgs[3], 10, 64)
		}
		count = 20 // 默认最多返回 20 条
		if len(extraArgs) > 4 {
			count, _ = strconv.ParseInt(extraArgs[4], 10, 64)
		}
		result, err := s.querier.QuerySortedSetByScore(index, keyValues, min, max, offset, count, subCmd == "rscore")
		if err != nil {
			return fmt.Sprintf("Error: %v", err)
		}
		return result

	default:
		return fmt.Sprintf("Unknown sorted set subcommand: %s\nAvailable: count, rank, rrank, score, rscore", subCmd)
	}
}
