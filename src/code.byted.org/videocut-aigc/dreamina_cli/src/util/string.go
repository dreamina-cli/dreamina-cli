package util

import "strings"

// FirstNonEmpty 返回第一个非空白字符串。
func FirstNonEmpty(v ...string) string {
	for _, item := range v {
		item = strings.TrimSpace(item)
		if item != "" {
			return item
		}
	}
	return ""
}

// TrimmedOrEmpty 返回去掉首尾空白后的字符串。
func TrimmedOrEmpty(v string) string {
	return strings.TrimSpace(v)
}
