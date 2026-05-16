# Rinha de Backend 2026 — Fraud Score API

API de detecção de fraude em transações financeiras, implementada em Go.

## Endpoints

### `GET /health`
Verifica se o servidor está no ar.

```
200 OK
ok
```

### `POST /fraud-score`
Recebe os dados de uma transação e retorna o score de fraude.

**Request body:**
```json
{
  "id": "tx-123",
  "transaction": {
    "amount": 41.12,
    "installments": 2,
    "requested_at": "2026-03-11T18:45:53Z"
  },
  "customer": {
    "avg_amount": 82.24,
    "tx_count_24h": 3,
    "known_merchants": ["MERC-003", "MERC-016"]
  },
  "merchant": {
    "id": "MERC-016",
    "mcc": "5411",
    "avg_amount": 60.25
  },
  "terminal": {
    "is_online": false,
    "card_present": true,
    "km_from_home": 29.23
  },
  "last_transaction": null
}
```

**Response:**
```json
{
  "approved": true,
  "fraud_score": 0.123
}
```

`approved` é `true` quando `fraud_score < 0.7`.

## Desenvolvimento

### Hot Reload com Air

O projeto usa [Air](https://github.com/air-verse/air) para hot reload durante o desenvolvimento. A configuração está em `.air.toml`: observa arquivos `.go` e reconstrói em `tmp/main` automaticamente.

```bash
make run       # inicia com hot reload
make build     # compila para bin/api
make test      # executa os testes unitários
make test-payloads  # dispara todos os payloads de exemplo contra o servidor local
```

### Testando os payloads

O alvo `test-payloads` usa o script `scripts/test-payloads.sh` que envia todos os 40 exemplos de `resources/example-payloads.json` para o endpoint `/fraud-score` e exibe um relatório com scores, decisões e estatísticas.

```bash
# servidor deve estar rodando antes
make run

# em outro terminal
make test-payloads

# URL e arquivo podem ser sobrescritos
FRAUD_SCORE_URL=http://staging:9999/fraud-score make test-payloads
FRAUD_SCORE_FILE=meu-arquivo.json make test-payloads
```

## Estrutura do projeto

```
.
├── main.go                        # entrypoint, rotas
├── .air.toml                      # configuração do hot reload
├── Makefile
├── resources/
│   ├── example-payloads.json      # 40 payloads de exemplo para testes
│   ├── mcc_risk.json              # tabela de risco por MCC (fonte canônica)
│   ├── normalization.json         # limites de normalização (fonte canônica)
│   └── example-references.json
├── internal/
│   ├── handler/
│   │   ├── fraud.go               # POST /fraud-score
│   │   └── util.go
│   ├── model/
│   │   └── fraud.go               # structs + validação
│   └── scoring/
│       ├── vectorize.go           # normalização em 14 dimensões
│       ├── vectorize_test.go
│       └── mcc_risk.json          # embed do risco por MCC
└── scripts/
    └── test-payloads.sh
```

## Scoring — Vetorização

`scoring.Vectorize` transforma uma requisição em 14 dimensões normalizadas `[0, 1]` (sentinela `-1` quando `last_transaction` é nulo):

| Dim | Nome                  | Normalização                          |
|-----|-----------------------|---------------------------------------|
| 0   | amount                | `amount / 10 000`                     |
| 1   | installments          | `installments / 12`                   |
| 2   | amount_vs_avg         | `(amount / avg_amount) / 10`          |
| 3   | hour_of_day           | `hora / 23`                           |
| 4   | day_of_week           | `(seg=0…dom=6) / 6`                   |
| 5   | minutes_since_last_tx | `minutos / 1 440` (ou `-1`)           |
| 6   | km_from_last_tx       | `km / 1 000` (ou `-1`)                |
| 7   | km_from_home          | `km / 1 000`                          |
| 8   | tx_count_24h          | `count / 20`                          |
| 9   | is_online             | `0` ou `1`                            |
| 10  | card_present          | `0` ou `1`                            |
| 11  | unknown_merchant      | `1` se merchant não é conhecido       |
| 12  | mcc_risk              | lookup em `mcc_risk.json`             |
| 13  | merchant_avg_amount   | `avg_amount / 10 000`                 |

Os limites de normalização vêm de `resources/normalization.json`. O risco por MCC vem de `resources/mcc_risk.json` (sincronizado com `internal/scoring/mcc_risk.json`).

**Próximo passo:** implementar o modelo KNN sobre o vetor para calcular o `fraud_score` real.

## Dependências

- [go-chi/chi](https://github.com/go-chi/chi) v5 — roteador HTTP
- [air-verse/air](https://github.com/air-verse/air) — hot reload (dev)
