# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## プロジェクト概要

llama-launcherは、llama.cppのランタイムとGGUFモデルを管理するGoベースのTUIアプリケーション。
llama-swapと組み合わせて使う。llama-swap自体の管理はスコープ外（ユーザーが別途インストール）。

### 責務の分担

- **llama-launcher**: ランタイム管理、モデル管理、プロファイル設定、ワークスペース管理、llama-swapの設定ファイル(YAML)生成・起動
- **llama-swap**: モデルのロード/アンロード、TTL、VRAM制御、OpenAI互換API提供

## 技術スタック

- 言語: Go
- TUIフレームワーク: [Bubble Tea](https://github.com/charmbracelet/bubbletea) + [huh](https://github.com/charmbracelet/huh) (フォーム)
- 配布: `go install github.com/desktopgame/llama-launcher/cmd/llama-launcher@latest`

## ビルド・開発コマンド

```bash
go build -o llama-launcher ./cmd/llama-launcher
go run ./cmd/llama-launcher
go test ./...
go test ./internal/runtime  # 単一パッケージのテスト
go test -run TestParseAssetName ./internal/runtime  # 単一テストの実行
gofmt -w .  # フォーマット
```

## アーキテクチャ

```
cmd/llama-launcher/     # エントリーポイント（TUIモード / ヘッドレスモード）
internal/
  config/               # アプリ設定 (モデル置き場パス、ポート番号等)
  runtime/              # llama.cppバイナリのダウンロード・バージョン管理 (GitHub Releases API)
  model/                # GGUFモデルの検出・管理 (HuggingFace検索、ローカルスキャン、LM Studio連携)
  profile/              # プロファイル (モデル + ランタイム + ModelType + 起動パラメータ)
  workspace/            # ワークスペース (プロファイルの組み合わせ + 常駐/非常駐設定)
  swap/                 # llama-swapのYAML設定生成・プロセス起動/停止
  tui/                  # Bubble Tea TUI
  util/                 # 共通ユーティリティ (ProgressReader等)
```

### 主要な概念

- **ランタイム**: llama.cppのビルド済みバイナリ。GitHubリリースからダウンロードし `{tag}-{backend}` 形式で複数バージョン・バックエンドを保持
- **モデル**: GGUFファイル。`model_dirs`（再帰スキャン）と`lmstudio_dir`（publisher/model-name レイアウト）から検出。mmprojは補助ファイルとして別管理
- **プロファイル**: モデル + ランタイム + ModelType(generation/embedding) + 起動パラメータ（ctx, ngl, flash-attn, mmap, mmproj, extra args）の組み合わせ
- **ワークスペース**: プロファイルの集合 + 各プロファイルの常駐/非常駐・TTL設定。llama-swapのconfig.yamlを生成する単位
- **llama-swap連携**: ワークスペースからconfig.yamlを一時ファイルに生成し、llama-swapプロセスを起動。常駐モデルはswap:falseグループ、非常駐はswap:trueグループに配置

### TUI設計上の注意

- Bubble Teaは値レシーバでUpdateを実装するため、huhフォームのバインド先は`*structPointer`で保持する必要がある（`profileFormValues`パターン）
- huhフォーム内蔵時は`Init()`の戻り値をCmdとして返す必要がある
- huhの`Select.Value()`と`Options()`の呼び出し順に注意（`Options()`呼び出し時にaccessorの値で初期選択が決まる）
- 子プロセスのstdout/stderrをos.Stdoutに流すとalt screenが壊れる。ログファイルに出力する

## 設計方針

- llama-swapは外部依存。このプロジェクトには含めず、ユーザーにインストールを委ねる
- モデルの保存先はユーザーが設定する。デフォルトの固定パスを押し付けない
- llama.cppのランタイムは複数バージョンを同時に保持でき、プロファイル単位で切り替え可能
- config.yamlはワークスペースから動的生成される一時ファイル。ディスクに永続化しない
- Windowsパスはconfig.yaml生成時にスラッシュに正規化する（YAMLエスケープ問題の回避）
