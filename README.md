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

### ワークスペース無しモード

```bash
llama-launcher --resident gemma4-31b,gemma4-26b-a4b-qat-mtp,qwen3-embedding [--ttl 900]
```

`profiles/`配下の全プロファイルを含めた上で、`--resident`に列挙したものだけを常駐にします。
ワークスペースを作らずに「どのモデルを常駐させるか」だけを外から決められるので、
常駐させるモデルを別のツール（`.env`など）で管理している場合の二重管理を避けられます。

壊れたプロファイル（モデルファイルが消えている、ランタイム未取得）の扱いは非対称です:

- `--resident`に**明示したもの** — 解決できなければエラーで起動しない
- **暗黙に含まれるもの** — 警告してスキップし、config.yamlから除外する

組み立てたワークスペースはディスクに保存しません。

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
  "port": 8080,
  "cost_max": 100
}
```

- **model_dirs** - モデルの検索ディレクトリ（再帰スキャン）。複数指定可能
- **lmstudio_dir** - LM Studioのモデルディレクトリ（publisher/model-name構造を認識）
- **default_backend** - ランタイムダウンロード時のデフォルトバックエンド
- **port** - llama-swapのリッスンポート
- **cost_max** - マシン全体のメモリ予算（単位を持たない抽象量）。0または未設定でチェック無効

## ワークスペースの仕組み

ワークスペースはプロファイルの組み合わせです。各プロファイルに対して:

- **常駐(resident)** - 他の非常駐モデルに追い出されない。TTLで自動アンロードはする
- **非常駐(on-demand)** - VRAMが足りなければ他の非常駐モデルと入れ替わる

起動するとllama-swapのconfig.yamlが自動生成され、llama-swapがリバースプロキシとして動作します。クライアントからは設定したポート(デフォルト8080)でOpenAI互換APIにアクセスできます。

## メモリ予算(cost)

llama-swapはメモリ容量を見てロードを止めてくれないため、載らない構成での起動はllama-launcher側で防ぎます。

プロファイルごとに`cost`（単位を持たない整数）を、`config.json`にマシン単位の`cost_max`を設定すると、起動前に次の式を検査します:

```
sum(常駐のcost) + max(非常駐のcost) <= cost_max
```

常駐グループは`swap: false`なので全部同時に載り、非常駐グループは`swap: true`なので同時に1つだけ載ります。よってピークは「常駐の合計 + 非常駐のうち最大の1つ」になります。

- `sum(常駐)`の超過は常時発生する定常状態なので**エラー**
- ピークの超過はそのモデルを実際に呼んだときにしか起きないので**警告**

`cost`はモデル単位ではなくプロファイル単位です。同じGGUFでもコンテキスト長やmmproj、ドラフトモデルの有無で消費量が変わるためです。
`vram_mb`のような名前にしていないのは、ユニファイドメモリ環境ではVRAMの境界が曖昧で、CPUオフロード構成でも同じ仕組みを使いたいからです。

`cost_max`が未設定(0)ならチェックは行われません。

### cost の実測

```bash
llama-launcher --measure                    # 全プロファイル
llama-launcher --measure gemma4-31b,qwen3-embedding   # 指定分だけ
```

各モデルを1つずつロードして、llama-swapの`/metrics`が返すシステムメモリ使用量の差分を取り、
`measured_cost`としてプロファイルに書き戻します。llama-swapはモデル単位のメモリメトリクスを
持たないため、「アンロード直後」と「ロード完了後」の差分で測ります。
ログ行のフォーマットに依存しないので、llama.cppのビルドが変わっても壊れません。

- 手で入れた`cost`は**絶対に上書きしません**。`--measure`が書くのは`measured_cost`だけです
- 実効cost = `cost`が設定されていればそれ、なければ`measured_cost`
- 測定は差分なので、他のプロセスがメモリを大きく動かしていると値が汚れます
- llama-swapが既に起動しているポートでは実行できません（メモリを排他的に使えないため）


### ドライラン

```bash
llama-launcher --resident gemma4-31b,qwen3-embedding --dry-run
llama-launcher myworkspace --dry-run
```

llama-swapを起動せずに、その構成が収まるかどうかだけを報告します。

```
Resident (all loaded at once)
  gemma4-31b                          29523  measured
  qwen3-embedding                      6350  measured
  total                               35873

On-demand (one at a time)
  gemma4-31b-qat-mtp                  28593  measured  <- peak
  ...

  peak                                64466
  cost_max                            92160
  headroom                            27694

  Fits within cost_max.
```

各costが実測値(`measured`)か手入力(`manual`)か未設定(`unset`)かも出ます。
config.yamlの生成まで実行するので、起動して初めて分かる種類の失敗（プロファイルが解決できない等）もここで検出できます。

終了コードは、常駐の合計が`cost_max`を超えていれば1、収まっていれば0です（ピークの超過は警告なので0）。

## 動作確認済み環境

- Windows 11

## ライセンス

MIT
