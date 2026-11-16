# ShortLink

Go と SQLite を使った短縮 URL サービスです。

## 起動

`.env.example` を `.env` にコピーし、管理者アカウントと32文字以上のセッション署名鍵を設定します。

```env
ADDR=:3000
APP_ENV=production
ADMIN_USERNAME=admin
ADMIN_PASSWORD=十分に長い推測されにくいパスワード
ADMIN_SESSION_KEY=32文字以上のランダムな秘密鍵
```

環境変数は `.env` より優先されます。.env は公開リポジトリへコミットしないでください。

```sh
go run .
```

本番環境では HTTPS を終端するリバースプロキシの背後で実行してください。管理者Cookieは HttpOnly、SameSite、Secure 属性を使用します。複数プロセスで動かす場合は、全プロセスで同じ `ADMIN_SESSION_KEY` を設定してください。データは作業ディレクトリ内の `data.db` に保存されます。

管理者パネルは `/admin` です。ユーザー名とパスワードは `ADMIN_USERNAME`、`ADMIN_PASSWORD` に設定した値を使います。URL一覧は10件ずつ表示し、コード・URL・作成元IP・Ban理由・警告理由で検索できます。Banと警告には理由が必要です。

## 設定

- `ADDR`: 待ち受けアドレス。既定値は `:3000`。
- `APP_ENV`: `production` では管理者Cookieに Secure 属性を付けます。ローカルHTTP開発時だけ `development` に設定してください。
- `ADMIN_USERNAME` / `ADMIN_PASSWORD`: 管理者ログイン情報。
- `ADMIN_SESSION_KEY`: セッション署名用の32文字以上の秘密鍵。
