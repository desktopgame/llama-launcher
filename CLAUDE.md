# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## プロジェクト概要

llama-launcherは、llama.cppのランタイムとGGUFモデルを管理するGoベースのTUIアプリケーション。
llama-swapと組み合わせて使う。llama-swap自体の管理はスコープ外（ユーザーが別途インストール）。

### 責務の分担

- **llama-launcher**: ランタイム管理、モデル管理、プロファイル設定、llama-swapの設定ファイル(YAML)生成・起動
- **llama-swap**: モデルのロード/アンロード、TTL、VRAM制御、OpenAI互換API提供

## 技術スタック

- 言語: Go
- TUIフレームワーク: [Bubble Tea](https://github.com/charmbracelet/bubbletea) (charmbracelet)
- 配布: `go install github.com/desktopgame/llama-launcher/cmd/llama-launcher@latest`

## ビルド・開発コマンド

```bash
go build -o llama-launcher ./cmd/llama-launcher
go run ./cmd/llama-launcher
go test ./...
go test ./internal/runtime  # 単一パッケージのテスト
go test -run TestDownload ./internal/runtime  # 単一テストの実行
```

## アーキテクチャ

```
cmd/llama-launcher/     # エントリーポイント
internal/
  runtime/              # llama.cppバイナリのダウンロード・バージョン管理 (GitHub Releases API)
  model/                # GGUFモデルの検出・管理
  profile/              # プロファイル (モデル + ランタイムバージョン + 起動パラメータの組み合わせ)
  swap/                 # llama-swapのYAML設定生成・プロセス起動/停止
  config/               # アプリ設定 (モデル置き場パス等)
  tui/                  # Bubble Tea TUI
```

### 主要な概念

- **ランタイム**: llama.cppのビルド済みバイナリ。GitHubリリースから複数バージョンをダウンロード・保持できる
- **モデル**: ユーザーが指定したディレクトリに配置されたGGUFファイル。置き場所はユーザーが自由に設定可能（他ツールのようにアプリ固定ディレクトリを強制しない）
- **プロファイル**: モデル + ランタイムバージョン + 起動パラメータ（コンテキストウィンドウ、mmapオプション等）の組み合わせ
- **llama-swap連携**: プロファイルからllama-swapのconfig.yamlを生成し、llama-swapプロセスを起動する。起動時にllama-swapがPATHに存在するか確認し、なければインストール方法を案内する

## 設計方針

- llama-swapは外部依存。このプロジェクトには含めず、ユーザーにインストールを委ねる
- モデルの保存先はユーザーが設定する。デフォルトの固定パスを押し付けない
- llama.cppのランタイムは複数バージョンを同時に保持でき、プロファイル単位で切り替え可能
