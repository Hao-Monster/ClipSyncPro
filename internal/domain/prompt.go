package domain

import (
	"crypto/rand"
	"encoding/hex"
	"regexp"
	"strings"
	"time"
)

// PromptTemplate 代表常用提示词/智能模板实体
type PromptTemplate struct {
	ID              string    `json:"id"`
	Title           string    `json:"title"`
	Category        string    `json:"category"`
	TemplateContent string    `json:"template_content"`
	Variables       []string  `json:"variables"`
	Shortcut        string    `json:"shortcut,omitempty"`
	UsageCount      int       `json:"usage_count"`
	IsFavorite      bool      `json:"is_favorite"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

var variableRegex = regexp.MustCompile(`\{([a-zA-Z0-9_\-:|]+)\}`)

// ExtractVariables 从模板内容中提取所有占位符变量
func ExtractVariables(content string) []string {
	matches := variableRegex.FindAllStringSubmatch(content, -1)
	if len(matches) == 0 {
		return []string{}
	}
	seen := make(map[string]struct{}, len(matches))
	var result []string
	for _, m := range matches {
		if len(m) >= 2 {
			varName := m[1]
			// 支持形如 {name:default} 或 {name:opt1|opt2}
			if idx := strings.Index(varName, ":"); idx != -1 {
				varName = varName[:idx]
			}
			if _, exists := seen[varName]; !exists {
				seen[varName] = struct{}{}
				result = append(result, varName)
			}
		}
	}
	return result
}

// NewPromptTemplate 创建新的提示词模板
func NewPromptTemplate(title, category, content, shortcut string) *PromptTemplate {
	buf := make([]byte, 16)
	_, _ = rand.Read(buf)
	now := time.Now().UTC()
	return &PromptTemplate{
		ID:              hex.EncodeToString(buf),
		Title:           strings.TrimSpace(title),
		Category:        strings.TrimSpace(category),
		TemplateContent: content,
		Variables:       ExtractVariables(content),
		Shortcut:        strings.TrimSpace(shortcut),
		UsageCount:      0,
		IsFavorite:      false,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
}

// DefaultPrompts 默认内置生产力提示词种子
func DefaultPrompts() []*PromptTemplate {
	return []*PromptTemplate{
		{
			ID:              "seed-code-review",
			Title:           "专家级 Code Review",
			Category:        "代码开发",
			TemplateContent: "请作为资深软件架构师，对以下代码进行细致的代码审查（Code Review），重点关注潜在 Bug、边界条件、并发安全、内存逃逸与性能瓶颈，并给出重构方案：\n\n```\n{clip}\n```",
			Variables:       []string{"clip"},
			Shortcut:        "/cr",
			UsageCount:      0,
			IsFavorite:      true,
			CreatedAt:       time.Now().UTC(),
			UpdatedAt:       time.Now().UTC(),
		},
		{
			ID:              "seed-translate-en-zh",
			Title:           "中英高质量互译",
			Category:        "翻译润色",
			TemplateContent: "请将以下内容翻译为地道、专业、自然的{target_lang:简体中文}，保留专有名词与技术术语不翻译：\n\n{clip}",
			Variables:       []string{"target_lang", "clip"},
			Shortcut:        "/trans",
			UsageCount:      0,
			IsFavorite:      true,
			CreatedAt:       time.Now().UTC(),
			UpdatedAt:       time.Now().UTC(),
		},
		{
			ID:              "seed-summarize-key-points",
			Title:           "核心要点与行动项提炼",
			Category:        "日常办公",
			TemplateContent: "请阅读以下内容，提炼出 3-5 条关键要点以及后续需要执行的明确行动清单（Action Items）：\n\n{clip}",
			Variables:       []string{"clip"},
			Shortcut:        "/sum",
			UsageCount:      0,
			IsFavorite:      false,
			CreatedAt:       time.Now().UTC(),
			UpdatedAt:       time.Now().UTC(),
		},
		{
			ID:              "seed-explain-code",
			Title:           "代码逐行原理解释",
			Category:        "代码开发",
			TemplateContent: "请清晰易懂地解释以下代码的设计思路与工作机制，说明其输入输出与关键算法逻辑：\n\n```\n{clip}\n```",
			Variables:       []string{"clip"},
			Shortcut:        "/explain",
			UsageCount:      0,
			IsFavorite:      false,
			CreatedAt:       time.Now().UTC(),
			UpdatedAt:       time.Now().UTC(),
		},
	}
}
