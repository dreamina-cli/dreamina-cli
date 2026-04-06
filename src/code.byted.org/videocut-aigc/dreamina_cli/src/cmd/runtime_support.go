package cmd

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"
)

type commandRunner func(cmd *Command, args []string) error

// AddCommand 把子命令追加到当前命令节点下。
func (c *Command) AddCommand(children ...*Command) {
	for _, child := range children {
		if child != nil {
			c.Children = append(c.Children, child)
		}
	}
}

// SetArgs 为命令设置本次执行用的参数切片。
func (c *Command) SetArgs(args []string) {
	c.args = append([]string(nil), args...)
}

// ExecuteC 执行当前命令节点，并在存在子命令时向下分发。
func (c *Command) ExecuteC() (*Command, error) {
	if _, ok := c.ctx.(context.Context); !ok {
		c.ctx = context.Background()
	}
	if _, ok := c.in.(io.Reader); !ok {
		c.in = os.Stdin
	}
	if _, ok := c.out.(io.Writer); !ok {
		c.out = os.Stdout
	}
	if len(c.args) == 0 {
		if strings.TrimSpace(c.Use) == "" || strings.TrimSpace(c.Use) == "dreamina" {
			return c, writeCommandHelp(c.OutOrStdout(), c)
		}
		if c.RunE != nil {
			return c, c.RunE(c, nil)
		}
		if c.out == nil {
			c.out = os.Stdout
		}
		_, _ = fmt.Fprintln(c.OutOrStdout(), commandUsage(c))
		return c, nil
	}

	if isHelpFlag(c.args[0]) {
		return c, writeCommandHelp(c.OutOrStdout(), c)
	}
	if isVersionFlag(c.args[0]) {
		return c, runVersion(c.OutOrStdout())
	}

	name := c.args[0]
	for _, child := range c.Children {
		if child != nil && child.Use == name {
			child.ctx = c.ctx
			child.in = c.in
			child.out = c.out
			if len(c.args) > 1 {
				if isHelpFlag(c.args[1]) {
					return child, writeCommandHelp(child.OutOrStdout(), child)
				}
				if isVersionFlag(c.args[1]) {
					return child, runVersion(child.OutOrStdout())
				}
			}
			if child.RunE != nil {
				return child, child.RunE(child, c.args[1:])
			}
			return child, nil
		}
	}

	if suggestion := suggestedCommandName(name, c.Children); suggestion != "" {
		return c, fmt.Errorf("unknown command %q for %q\n\nDid you mean this?\n\t%s\n", name, strings.TrimSpace(c.Use), suggestion)
	}
	return c, fmt.Errorf("unknown command %q for %q", name, strings.TrimSpace(c.Use))
}

// Context 返回命令上下文；缺失时自动补默认背景上下文。
func (c *Command) Context() context.Context {
	if ctx, ok := c.ctx.(context.Context); ok {
		return ctx
	}
	c.ctx = context.Background()
	return c.ctx.(context.Context)
}

// InOrStdin 返回命令输入流；缺失时回退到标准输入。
func (c *Command) InOrStdin() io.Reader {
	if in, ok := c.in.(io.Reader); ok {
		return in
	}
	c.in = os.Stdin
	return c.in.(io.Reader)
}

// OutOrStdout 返回命令输出流；缺失时回退到标准输出。
func (c *Command) OutOrStdout() io.Writer {
	if out, ok := c.out.(io.Writer); ok {
		return out
	}
	c.out = os.Stdout
	return c.out.(io.Writer)
}

func suggestedCommandName(name string, children []*Command) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	normalized := strings.ReplaceAll(name, "-", "_")
	for _, child := range children {
		if child == nil {
			continue
		}
		use := strings.TrimSpace(child.Use)
		if use == "" {
			continue
		}
		if normalized == use || strings.ReplaceAll(use, "_", "-") == name {
			return use
		}
	}
	return ""
}
