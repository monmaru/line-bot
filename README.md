# line-bot

## LINE Bot APIの設定
LINEのビジネスアカウントを作成し、Botを作成する。  
作成方法はググってください。  
line.envに各自の設定を記載する。  
- CHANNEL_SECRET
- CHANNEL_TOKEN
- QR_URL

## GAEへのデプロイ
本ディレクトリで下記コマンドを実行してください。  
```
appcfg.py -A PROJECT-ID -V VERSION update .
```