#!/usr/bin/env bash
export LC_NUMERIC=C

URL="${FRAUD_SCORE_URL:-http://localhost:9999/fraud-score}"
FILE="${FRAUD_SCORE_FILE:-resources/example-payloads.json}"
CONCURRENCY="${CONCURRENCY:-4}"
REQUESTS="${REQUESTS:-200}"

BOLD='\033[1m'
RESET='\033[0m'
GREEN='\033[32m'
RED='\033[31m'
YELLOW='\033[33m'
CYAN='\033[36m'
MAGENTA='\033[35m'
DIM='\033[2m'

tmpdir=$(mktemp -d)
trap 'rm -rf "$tmpdir"' EXIT

if ! command -v jq &>/dev/null; then
    echo "erro: jq não encontrado" >&2; exit 1
fi
if [ ! -f "$FILE" ]; then
    echo "erro: $FILE não encontrado" >&2; exit 1
fi

# ── Configuração do servidor ──────────────────────────────────────────────────

BASE_URL="${URL%%/fraud-score*}"
ready_json=$(curl -s --max-time 3 "${BASE_URL}/ready" 2>/dev/null)
srv_workers=$(echo "$ready_json" | jq -r '.workers  // "?"' 2>/dev/null)
srv_queue=$(echo   "$ready_json" | jq -r '.queue_cap // "?"' 2>/dev/null)

# ── Extrai payloads para arquivos individuais ─────────────────────────────────

total_payloads=$(jq length "$FILE")
for i in $(seq 0 $((total_payloads - 1))); do
    jq ".[$i]" "$FILE" > "$tmpdir/p_${i}.json"
done

# ── Cabeçalho ─────────────────────────────────────────────────────────────────

echo ""
printf "${BOLD}╔══════════════════════════════════════════════════════╗${RESET}\n"
printf "${BOLD}║       Rinha Backend — Benchmark Concorrente          ║${RESET}\n"
printf "${BOLD}╚══════════════════════════════════════════════════════╝${RESET}\n"
printf "${DIM}  endpoint    : %s${RESET}\n" "$URL"
printf "${YELLOW}  workers     : %s${RESET}\n" "$srv_workers"
printf "${CYAN}  concorrência: %d${RESET}\n" "$CONCURRENCY"
printf "${DIM}  requisições : %d${RESET}\n" "$REQUESTS"
echo ""
printf "  ${DIM}Executando...${RESET}\n"
echo ""

# ── Execução paralela ─────────────────────────────────────────────────────────
# Variáveis do pai são interpoladas no momento da chamada (double quotes).
# __N__ é substituído pelo xargs com o índice de cada requisição.
# O resultado sempre é escrito no arquivo — fallback "000|0.000000" cobre falhas do curl.

start_epoch=$(date +%s%3N)

seq 0 $((REQUESTS - 1)) | \
    xargs -P "$CONCURRENCY" -I __N__ \
    bash -c '
        n=__N__
        pidx=$(( n % '"$total_payloads"' ))
        result=$(curl -s -o /dev/null \
            -w "%{http_code}|%{time_total}" \
            -X POST "'"$URL"'" \
            -H "Content-Type: application/json" \
            --data-binary @"'"$tmpdir"'/p_${pidx}.json" \
            2>/dev/null)
        printf "%s\n" "${result:-000|0.000000}" > "'"$tmpdir"'/r_${n}.txt"
    '

end_epoch=$(date +%s%3N)
total_wall_ms=$((end_epoch - start_epoch))
total_wall_s=$(awk "BEGIN{printf \"%.3f\", $total_wall_ms / 1000}")

# ── Agrega resultados — sem eval ──────────────────────────────────────────────

# Tempos das respostas 2xx para percentis
awk -F'|' 'NF==2 && /^2/{print $2}' "$tmpdir"/r_*.txt 2>/dev/null \
    | sort -n > "$tmpdir/times.txt"

n_times=$(wc -l < "$tmpdir/times.txt" | tr -d ' ')
ok_count=$n_times
err_count=$(( REQUESTS - ok_count ))

# Estatísticas básicas via awk
read -r avg_s time_min_s time_max_s < <(awk '
    BEGIN { min=999999; max=0; sum=0; n=0 }
    { sum+=$1; n++; if($1<min) min=$1; if($1>max) max=$1 }
    END { if(n>0) printf "%.6f %.6f %.6f\n", sum/n, min, max
          else    print "0 0 0" }
' "$tmpdir/times.txt")

# Códigos de erro agrupados
err_summary=$(awk -F'|' 'NF==2 && !/^2/{c[$1]++} END{for(s in c) printf "%s=%d ", s, c[s]}' \
    "$tmpdir"/r_*.txt 2>/dev/null)

# Percentis (índice 1-based na lista ordenada)
pct() {
    local pct=$1
    local line
    line=$(awk "BEGIN{n=int($n_times*$pct); print (n<1?1:(n>$n_times?$n_times:n))}")
    sed -n "${line}p" "$tmpdir/times.txt"
}

if [ "$n_times" -gt 0 ]; then
    avg_ms=$(awk  "BEGIN{printf \"%.1f\", $avg_s     * 1000}")
    min_ms=$(awk  "BEGIN{printf \"%.0f\", $time_min_s * 1000}")
    max_ms=$(awk  "BEGIN{printf \"%.0f\", $time_max_s * 1000}")
    p50_ms=$(awk  "BEGIN{printf \"%.0f\", $(pct 0.50) * 1000}")
    p95_ms=$(awk  "BEGIN{printf \"%.0f\", $(pct 0.95) * 1000}")
    p99_ms=$(awk  "BEGIN{printf \"%.0f\", $(pct 0.99) * 1000}")
fi

throughput=$(awk "BEGIN{printf \"%.2f\", $REQUESTS / ($total_wall_s + 0.001)}")

# ── Resultados finais ─────────────────────────────────────────────────────────

echo ""
printf "${BOLD}╔══════════════════════════════════════════════════════╗${RESET}\n"
printf "${BOLD}║                    RESULTADOS                        ║${RESET}\n"
printf "${BOLD}╚══════════════════════════════════════════════════════╝${RESET}\n"
echo ""

ok_pct=$(awk  "BEGIN{printf \"%.1f\", ($ok_count  / $REQUESTS) * 100}")
err_pct=$(awk "BEGIN{printf \"%.1f\", ($err_count / $REQUESTS) * 100}")

printf "  ${BOLD}%-20s${RESET} %d\n" "Total:" "$REQUESTS"
printf "  ${GREEN}%-20s${RESET} %-4d  (${GREEN}%s%%${RESET})\n" "Sucesso:" "$ok_count" "$ok_pct"
if [ "$err_count" -gt 0 ]; then
    printf "  ${RED}%-20s${RESET} %-4d  (${RED}%s%%${RESET})  ${DIM}%s${RESET}\n" \
        "Erros:" "$err_count" "$err_pct" "$err_summary"
fi

if [ "$n_times" -gt 0 ]; then
    echo ""
    printf "  ${BOLD}Latência${RESET}  ${DIM}(sobre %d respostas 2xx)${RESET}\n" "$n_times"
    printf "    ${DIM}%-10s${RESET}  ${BOLD}%s ms${RESET}\n"   "Média:"  "$avg_ms"
    printf "    ${DIM}%-10s${RESET}  ${GREEN}%s ms${RESET}\n"  "Mínima:" "$min_ms"
    printf "    ${DIM}%-10s${RESET}  %s ms\n"                  "p50:"    "$p50_ms"
    printf "    ${DIM}%-10s${RESET}  ${YELLOW}%s ms${RESET}\n" "p95:"    "$p95_ms"
    printf "    ${DIM}%-10s${RESET}  ${RED}%s ms${RESET}\n"    "p99:"    "$p99_ms"
    printf "    ${DIM}%-10s${RESET}  ${RED}%s ms${RESET}\n"    "Máxima:" "$max_ms"
fi

echo ""
printf "  ${BOLD}${MAGENTA}Desempenho${RESET}\n"
printf "    ${DIM}%-10s${RESET}  ${BOLD}%s req/s${RESET}\n" "Throughput:" "$throughput"
printf "    ${DIM}%-10s${RESET}  ${CYAN}%s s${RESET}\n"     "Total:"      "$total_wall_s"

echo ""
printf "${DIM}══════════════════════════════════════════════════════${RESET}\n"
echo ""
