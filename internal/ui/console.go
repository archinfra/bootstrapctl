package ui

import (
	"fmt"
	"io"
	"os"
	"strings"
)

const (
	colorReset  = "\033[0m"
	colorRed    = "\033[0;31m"
	colorGreen  = "\033[0;32m"
	colorYellow = "\033[1;33m"
	colorBlue   = "\033[0;34m"
	colorCyan   = "\033[0;36m"
)

// Console 负责统一 CLI 输出风格。
// 这里不依赖第三方终端库，避免给离线交付引入额外依赖。
type Console struct {
	out io.Writer
	err io.Writer
}

func NewConsole() *Console {
	return &Console{
		out: os.Stdout,
		err: os.Stderr,
	}
}

func (c *Console) Banner(title string) {
	line := strings.Repeat("=", max(12, len([]rune(title))+8))
	fmt.Fprintf(c.out, "\n%s%s%s\n", colorBlue, line, colorReset)
	fmt.Fprintf(c.out, "%s%s%s\n", colorBlue, title, colorReset)
	fmt.Fprintf(c.out, "%s%s%s\n\n", colorBlue, line, colorReset)
}

func (c *Console) Section(title string) {
	fmt.Fprintf(c.out, "\n%s[%s]%s\n", colorBlue, title, colorReset)
}

func (c *Console) Item(label string, value any) {
	fmt.Fprintf(c.out, "  - %s: %v\n", label, value)
}

func (c *Console) Command(command string) {
	fmt.Fprintf(c.out, "    %s$ %s%s\n", colorBlue, command, colorReset)
}

func (c *Console) Info(format string, args ...any) {
	c.printf(c.out, colorCyan, "信息", format, args...)
}

func (c *Console) Success(format string, args ...any) {
	c.printf(c.out, colorGreen, "成功", format, args...)
}

func (c *Console) Warn(format string, args ...any) {
	c.printf(c.out, colorYellow, "警告", format, args...)
}

func (c *Console) Error(format string, args ...any) {
	// 统一输出到 stdout，避免 stdout/stderr 交错后影响阅读顺序。
	c.printf(c.out, colorRed, "错误", format, args...)
}

func (c *Console) Plain(format string, args ...any) {
	fmt.Fprintf(c.out, format+"\n", args...)
}

func (c *Console) printf(w io.Writer, color, level, format string, args ...any) {
	fmt.Fprintf(w, "%s[%s]%s %s\n", color, level, colorReset, fmt.Sprintf(format, args...))
}

func max(left, right int) int {
	if left > right {
		return left
	}
	return right
}
