# Rinha de Backend 2026 — Guia do Projeto

## O Desafio

A **Rinha de Backend 2026** é uma competição de backend cujo tema é **detecção de fraude em transações financeiras via busca vetorial**.

Referência oficial: https://github.com/zanfranceschi/rinha-de-backend-2026

Cada participante deve construir uma API que, dado o payload de uma transação, vetoriza os dados, busca os vizinhos mais próximos em um dataset de referência com ~3 milhões de transações rotuladas (`fraud` / `legit`) e decide se a transação deve ser aprovada ou negada.

---

## API

### Porta obrigatória: `9999`

| Método | Path           | Descrição                                  |
|--------|----------------|--------------------------------------------|
| GET    | `/ready`       | Healthcheck — responde 2xx quando pronto   |
| POST   | `/fraud-score` | Recebe transação, retorna score e decisão  |

> Nossa implementação expõe `/health` em vez de `/ready` — ajustar para a spec final.

### Request `POST /fraud-score`

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

### Response padrão

```json
{
  "approved": true,
  "fraud_score": 0.2
}
```

---

## Lógica de Detecção

### 1. Vetorização (14 dimensões, `[0, 1]`, sentinela `-1` para ausência)

| Dim | Feature               | Normalização                        |
|-----|-----------------------|-------------------------------------|
| 0   | amount                | `amount / 10 000`                   |
| 1   | installments          | `installments / 12`                 |
| 2   | amount_vs_avg         | `(amount / customer.avg_amount) / 10` |
| 3   | hour_of_day           | `hora / 23`                         |
| 4   | day_of_week           | `(seg=0…dom=6) / 6`                 |
| 5   | minutes_since_last_tx | `minutos / 1 440` (ou `-1`)         |
| 6   | km_from_last_tx       | `km / 1 000` (ou `-1`)              |
| 7   | km_from_home          | `km / 1 000`                        |
| 8   | tx_count_24h          | `count / 20`                        |
| 9   | is_online             | `0.0` ou `1.0`                      |
| 10  | card_present          | `0.0` ou `1.0`                      |
| 11  | unknown_merchant      | `1.0` se merchant não está em `known_merchants` |
| 12  | mcc_risk              | lookup em `mcc_risk.json`           |
| 13  | merchant_avg_amount   | `avg_amount / 10 000`               |

Dimensões 5 e 6 usam sentinela `-1` quando `last_transaction` é nulo.
Na distância euclidiana, par (-1, -1) contribui 0; par (-1, x) contribui 1.

### 2. KNN brute-force

- K = 5 vizinhos
- Distância: euclidiana com tratamento de sentinela
- Dataset: `resources/references.json.gz` (~3M vetores rotulados), embutido via `//go:embed`

### 3. Score e decisão

```
fraud_score = fraudulent_neighbors / K
approved    = fraud_score < 0.7
```

> A spec oficial usa `< 0.6`; nosso threshold atual é `0.7` — revisar antes da submissão.

---

## Arquivos de Referência (estáticos durante os testes)

| Arquivo                        | Uso                                            |
|--------------------------------|------------------------------------------------|
| `resources/references.json.gz` | 3M vetores rotulados (fraud/legit), embutido   |
| `resources/mcc_risk.json`      | Risco por MCC (fonte canônica)                 |
| `resources/normalization.json` | Constantes de normalização (fonte canônica)    |

> `internal/scoring/mcc_risk.json` é cópia do canônico — manter sincronizados.

---

## Infraestrutura Exigida pelo Desafio

- **1 load balancer** + **2 instâncias da API** (round-robin)
- **Limite total:** ≤ 1 CPU, ≤ 350 MB de RAM
- **Docker Compose** obrigatório; imagens linux/amd64 públicas
- Rede em modo bridge

## Configuração de Runtime (ponto ótimo empírico)

| Variável      | Valor | Motivo                                                                 |
|---------------|-------|------------------------------------------------------------------------|
| `WORKERS`     | `4`   | Minimiza p99; verificado empiricamente                                 |
| `GOMAXPROCS`  | `4`   | Permite paralelismo real nos chunks do KNN; padrão Go seria 1 no cgroup |

Não alterar esses valores sem nova bateria de testes de carga. Definidos como `ENV` no Dockerfile e como variáveis de ambiente em `api1`/`api2` no `docker-compose.yml`.

---

## Pontuação (máximo ±6000 pts)

| Critério     | Peso    | Regra                                                   |
|--------------|---------|---------------------------------------------------------|
| Latência     | ±3 000  | p99 ≤ 1 ms → +3 000; p99 > 2 000 ms → -3 000           |
| Detecção     | ±3 000  | Taxa de erro > 15% → -3 000                             |

Falsos positivos, falsos negativos e erros HTTP são penalizados no critério de detecção.

---

## Logging

Todos os logs da aplicação passam pelo pacote `internal/logger`. **Sempre use cores ANSI** — nunca `log.Printf` direto nos handlers ou em `main.go`.

| Tag         | Cor     | Função                  | Uso                            |
|-------------|---------|-------------------------|--------------------------------|
| `[startup]` | magenta | `logger.Startup`        | inicialização do servidor      |
| `[http   ]` | dim     | `logger.HTTP` / middleware | acesso HTTP (via middleware) |
| `[enqueue]` | cyan    | `logger.Enqueue`        | job enfileirado                |
| `[worker+]` | yellow  | `logger.WorkerIn`       | worker começa a processar      |
| `[worker-]` | green   | `logger.WorkerOut`      | worker termina                 |
| `[ready  ]` | blue    | `logger.Ready`          | health check com estado da fila|
| `[reject ]` | red     | `logger.Reject`         | fila cheia, req rejeitada      |

## Desenvolvimento

```bash
make run            # hot reload com Air
make build          # compila bin/api
make test           # testes unitários
make test-payloads  # dispara 40 payloads de exemplo contra :9999
```

---

## Estrutura

```
.
├── main.go                         # entrypoint, rotas, embed do dataset
├── internal/
│   ├── handler/fraud.go            # POST /fraud-score
│   ├── model/fraud.go              # structs + validação de entrada
│   └── scoring/
│       ├── vectorize.go            # normalização → vetor [14]float64
│       ├── knn.go                  # KNN brute-force
└── resources/
    ├── references.json.gz          # dataset de referência (embutido)
    ├── mcc_risk.json               # risco por MCC — embed em main.go
    └── normalization.json
```

---

## Comentários no Código

Adicione comentários educacionais sempre que o trecho envolver:

- **Restrições não óbvias da linguagem** (ex: limitação do `go:embed` com paths `..`)
- **Padrões de concorrência** (worker pool, atomic, canais com/sem buffer)
- **Algoritmos com trade-offs** (ex: brute-force KNN vs. índice aproximado)
- **Decisões de design que não aparecem no nome das funções**

Comentários devem explicar o **porquê** e o **trade-off**, não o que o código faz literalmente.
Nunca escreva blocos de comentário genéricos do tipo "esta função faz X" — apenas quando a razão for surpreendente para um leitor de Go experiente.
