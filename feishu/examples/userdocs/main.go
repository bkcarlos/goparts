// userdocs is an explicit live example. It does not import or execute lark-cli.
package main

import (
	"context"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/bkcarlos/goparts/feishu/user"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run() error {
	if len(os.Args) < 2 || os.Args[1] == "-h" || os.Args[1] == "--help" {
		fmt.Println("用法: userdocs <login|whoami|read|create|append|update|logout> [参数]")
		fmt.Println("环境变量: FEISHU_APP_ID, FEISHU_APP_SECRET, FEISHU_TOKEN_KEY (64 位十六进制，固定复用)")
		fmt.Println("可选: FEISHU_SCOPES (默认 docx:document), FEISHU_API_BASE_URL, FEISHU_ACCOUNTS_URL")
		return nil
	}
	command := os.Args[1]
	switch command {
	case "login", "whoami", "read", "create", "append", "update", "logout":
	default:
		return errors.New("未知命令；使用 --help 查看用法")
	}
	flags := flag.NewFlagSet(command, flag.ContinueOnError)
	session := flags.String("session", "", "加密会话文件路径；默认用户配置目录下 github.com/bkcarlos/goparts/feishu-user/session.enc")
	docID := flags.String("doc", "", "文档 ID")
	blockID := flags.String("block", "", "待更新块 ID")
	title := flags.String("title", "", "新文档标题")
	folder := flags.String("folder", "", "目标文件夹 token")
	text := flags.String("text", "", "追加或替换的文本")
	clientToken := flags.String("client-token", "", "可选幂等操作 ID")
	if err := flags.Parse(os.Args[2:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil
		}
		return err
	}
	if flags.NArg() != 0 {
		return errors.New("存在未识别的位置参数")
	}
	key, err := hex.DecodeString(os.Getenv("FEISHU_TOKEN_KEY"))
	if err != nil || len(key) != 32 {
		return errors.New("请配置 FEISHU_TOKEN_KEY：32 字节密钥的 64 位十六进制编码，并在后续运行中复用同一密钥")
	}
	if *session == "" {
		dir, err := os.UserConfigDir()
		if err != nil {
			return err
		}
		*session = filepath.Join(dir, "go_sdk", "feishu-user", "session.enc")
	}
	store, err := user.NewEncryptedFileStore(*session, key)
	if err != nil {
		return err
	}
	scope := os.Getenv("FEISHU_SCOPES")
	if scope == "" {
		scope = user.ScopeWriteDocuments
	}
	c, err := user.New(user.Config{AppID: os.Getenv("FEISHU_APP_ID"), AppSecret: os.Getenv("FEISHU_APP_SECRET"), Scopes: strings.Fields(strings.ReplaceAll(scope, ",", " ")), BaseURL: os.Getenv("FEISHU_API_BASE_URL"), AccountsURL: os.Getenv("FEISHU_ACCOUNTS_URL"), Store: store})
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	opts := user.WriteOptions{ClientToken: *clientToken}
	switch command {
	case "login":
		auth, err := c.StartLogin(ctx)
		if err != nil {
			return err
		}
		fmt.Println("请在浏览器或飞书中打开链接，确认登录用户和授权范围：")
		fmt.Println(auth.VerificationURIComplete)
		fmt.Printf("用户码: %s\n", auth.UserCode)
		identity, err := c.CompleteLogin(ctx, auth)
		if err != nil {
			return err
		}
		fmt.Printf("已登录: %s (%s)\n", identity.Name, identity.OpenID)
	case "whoami":
		identity, err := c.Me(ctx)
		if err != nil {
			return err
		}
		fmt.Printf("%s (%s)\n", identity.Name, identity.OpenID)
	case "read":
		content, err := c.ReadDocument(ctx, *docID)
		if err != nil {
			return err
		}
		fmt.Println(content)
	case "create":
		doc, err := c.CreateDocument(ctx, *title, *folder)
		if err != nil {
			return err
		}
		fmt.Printf("document_id=%s title=%s\n", doc.ID, doc.Title)
	case "append":
		result, err := c.AppendText(ctx, *docID, *text, opts)
		if err != nil {
			return err
		}
		fmt.Printf("已追加，revision=%d\n", result.RevisionID)
	case "update":
		result, err := c.UpdateText(ctx, *docID, *blockID, *text, opts)
		if err != nil {
			return err
		}
		fmt.Printf("已更新，revision=%d\n", result.RevisionID)
	case "logout":
		if err := c.Logout(ctx); err != nil {
			return err
		}
		fmt.Println("已清除本地会话")
	}
	return nil
}
