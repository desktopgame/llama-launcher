# llama-launcher

llama.cppのランタイムとGGUFモデルを管理するTUIアプリケーション。[llama-swap](https://github.com/mostlygeek/llama-swap)と組み合わせて使います。

LM Studioのような使いやすさを目指しつつ、llama.cppの最新リリースに素早く追従できることを重視しています。

## 特徴

- **ランタイム管理** - llama.cppのビルド済みバイナリをGitHubリリースからダウンロード。複数バージョン・バックエンド(Vulkan, CUDA, ROCm等)を同時に保持
- **モデル管理** - HuggingFaceからGGUFモデルを検索・ダウンロード。ローカルのモデルディレクトリを自由に設定可能（LM Studioのモデルも参照可能）
- **プロファイル** - モデル + ランタイム + 起動パラメータ(ctx, ngl, flash-attn等)の組み合わせを保存。任意のllama-serverオプションも追加可能
- **ワークスペース** - 複数プロファイルを常駐/非常駐で組み合わせ、llama-swapのconfig.yamlを自動生成して起動
- **ヘッドレスモード** - ワークスペース名を指定して即サーバー起動

## インストール

```bash
go install github.com/desktopgame/llama-launcher/cmd/llama-launcher@latest
```

llama-swapも別途インストールが必要です:
```bash
go install github.com/mostlygeek/llama-swap@latest
```

## 使い方

### TUIモード

```bash
llama-launcher
```

メインメニューから各機能にアクセスできます:

- **Download Runtime** - llama.cppのリリースを選択してダウンロード
- **Installed Runtimes** - ダウンロード済みランタイムの管理
- **Search Models** - HuggingFaceでGGUFモデルを検索・ダウンロード
- **Local Models** - ローカルモデルの一覧（タブでディレクトリ切替）
- **Profiles** - プロファイルの作成・編集・削除（右パネルに詳細表示）
- **Workspaces** - ワークスペースの作成・編集・起動・停止
- **Settings** - 設定の確認・設定フォルダを開く

### ヘッドレスモード

```bash
llama-launcher <ワークスペース名>
```

TUIなしで即座にllama-swapを起動します。Ctrl+Cで停止。

## 設定

設定ファイルは初回起動時に自動生成されます（Settingsからフォルダを開けます）。

```json
{
  "model_dirs": ["D:/models/gguf"],
  "lmstudio_dir": "C:/Users/username/.lmstudio/models",
  "runtime_dir": "...",
  "profile_dir": "...",
  "workspace_dir": "...",
  "default_backend": "vulkan",
  "port": 8080
}
```

- **model_dirs** - モデルの検索ディレクトリ（再帰スキャン）。複数指定可能
- **lmstudio_dir** - LM Studioのモデルディレクトリ（publisher/model-name構造を認識）
- **default_backend** - ランタイムダウンロード時のデフォルトバックエンド
- **port** - llama-swapのリッスンポート

## ワークスペースの仕組み

ワークスペースはプロファイルの組み合わせです。各プロファイルに対して:

- **常駐(resident)** - 他の非常駐モデルに追い出されない。TTLで自動アンロードはする
- **非常駐(on-demand)** - VRAMが足りなければ他の非常駐モデルと入れ替わる

起動するとllama-swapのconfig.yamlが自動生成され、llama-swapがリバースプロキシとして動作します。クライアントからは設定したポート(デフォルト8080)でOpenAI互換APIにアクセスできます。

## 動作確認済み環境

- Windows 11

## ライセンス

MIT
