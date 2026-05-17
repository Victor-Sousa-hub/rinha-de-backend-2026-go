#!/usr/bin/env bash
export LC_NUMERIC=C

URL="${FRAUD_SCORE_URL:-http://localhost:9999/fraud-score}"
FILE="${FRAUD_SCORE_FILE:-resources/example-payloads.json}"

BOLD='\033[1m'
RESET='\033[0m'
GREEN='\033[32m'
RED='\033[31m'
YELLOW='\033[33m'
CYAN='\033[36m'
BLUE='\033[34m'
MAGENTA='\033[35m'
DIM='\033[2m'

tmpfile=$(mktemp)
trap 'rm -f "$tmpfile"' EXIT

if ! command -v jq &>/dev/null; then
    echo "erro: jq não encontrado" >&2
    exit 1
fi

if [ ! -f "$FILE" ]; then
    echo "erro: $FILE não encontrado" >&2
    exit 1
fi

total=$(jq length "$FILE")

ok_count=0
err_count=0
approved_count=0
rejected_count=0
score_sum="0"
score_min=""
score_max=""
time_sum_s="0"
time_min_s=""
time_max_s=""
declare -A status_map

start_epoch=$(date +%s%3N)

# Consulta /ready para obter config ativa do servidor
BASE_URL="${URL%%/fraud-score*}"
READY_URL="${BASE_URL}/ready"
ready_json=$(curl -s --max-time 3 "$READY_URL" 2>/dev/null)
srv_workers=$(echo "$ready_json" | jq -r '.workers  // "?"' 2>/dev/null)
srv_queue=$(echo   "$ready_json" | jq -r '.queue_cap // "?"' 2>/dev/null)

echo ""
printf "${BOLD}╔══════════════════════════════════════════════════════╗${RESET}\n"
printf "${BOLD}║        Rinha Backend — Fraud Score Test Suite        ║${RESET}\n"
printf "${BOLD}╚══════════════════════════════════════════════════════╝${RESET}\n"
printf "${DIM}  endpoint : %s${RESET}\n" "$URL"
printf "${DIM}  payloads : %d em %s${RESET}\n" "$total" "$FILE"
printf "${YELLOW}  workers  : %s${RESET}\n" "$srv_workers"
printf "${DIM}  queue_cap: %s${RESET}\n" "$srv_queue"
echo ""

for i in $(seq 0 $((total - 1))); do
    payload=$(jq ".[$i]" "$FILE")
    tx_id=$(echo "$payload" | jq -r '.id')
    has_last_tx=$(echo "$payload" | jq -r 'if .last_transaction != null then "sim" else "não" end')

    result=$(echo "$payload" | curl -s -o "$tmpfile" -w "%{http_code}|%{time_total}" \
        -X POST "$URL" \
        -H 'Content-Type: application/json' \
        -d @-)
    http_status="${result%%|*}"
    req_time_s="${result##*|}"
    body=$(cat "$tmpfile")

    req_time_ms=$(awk "BEGIN{printf \"%.0f\", $req_time_s * 1000}")
    time_sum_s=$(awk "BEGIN{printf \"%.6f\", $time_sum_s + $req_time_s}")

    if [ -z "$time_min_s" ]; then
        time_min_s="$req_time_s"
        time_max_s="$req_time_s"
    else
        time_min_s=$(awk "BEGIN{print ($req_time_s < $time_min_s) ? $req_time_s : $time_min_s}")
        time_max_s=$(awk "BEGIN{print ($req_time_s > $time_max_s) ? $req_time_s : $time_max_s}")
    fi

    status_map["$http_status"]=$(( ${status_map["$http_status"]:-0} + 1 ))
    num=$((i + 1))

    if [ "$http_status" = "200" ]; then
        ok_count=$((ok_count + 1))
        score=$(echo "$body" | jq -r '.fraud_score')
        approved=$(echo "$body" | jq -r '.approved')

        score_sum=$(awk "BEGIN{printf \"%.6f\", $score_sum + $score}")

        if [ -z "$score_min" ]; then
            score_min="$score"
            score_max="$score"
        else
            score_min=$(awk "BEGIN{print ($score < $score_min) ? $score : $score_min}")
            score_max=$(awk "BEGIN{print ($score > $score_max) ? $score : $score_max}")
        fi

        if [ "$approved" = "true" ]; then
            approved_count=$((approved_count + 1))
            decision="${GREEN}✓ aprovado${RESET}"
        else
            rejected_count=$((rejected_count + 1))
            decision="${RED}✗ negado  ${RESET}"
        fi

        score_f=$(printf "%.3f" "$score")
        score_color="${GREEN}"
        if awk "BEGIN{exit !($score >= 0.5)}"; then score_color="${YELLOW}"; fi
        if awk "BEGIN{exit !($score >= 0.7)}"; then score_color="${RED}"; fi

        hist_label="${DIM}sem hist. ${RESET}"
        [ "$has_last_tx" = "sim" ] && hist_label="${CYAN}com hist. ${RESET}"

        printf "  ${DIM}[%3d/%d]${RESET}  ${BOLD}%-18s${RESET}  ${BLUE}%s${RESET}  score ${score_color}%s${RESET}  %b  %b  ${DIM}%sms${RESET}\n" \
            "$num" "$total" "$tx_id" "$http_status" "$score_f" "$decision" "$hist_label" "$req_time_ms"
    else
        err_count=$((err_count + 1))
        fields=$(echo "$body" | jq -r 'if .fields then (.fields | join(", ")) else (.error // "erro desconhecido") end')
        printf "  ${DIM}[%3d/%d]${RESET}  ${BOLD}%-18s${RESET}  ${RED}%s${RESET}  %s  ${DIM}%sms${RESET}\n" \
            "$num" "$total" "$tx_id" "$http_status" "$fields" "$req_time_ms"
    fi
done

end_epoch=$(date +%s%3N)
total_wall_ms=$((end_epoch - start_epoch))
total_wall_s=$(awk "BEGIN{printf \"%.3f\", $total_wall_ms / 1000}")

# ── Resumo final ──────────────────────────────────────────────────────────────

echo ""
printf "${BOLD}╔══════════════════════════════════════════════════════╗${RESET}\n"
printf "${BOLD}║                   RESULTADOS FINAIS                  ║${RESET}\n"
printf "${BOLD}╚══════════════════════════════════════════════════════╝${RESET}\n"
echo ""

printf "  ${BOLD}%-26s${RESET} %d\n" "Total de requisições:" "$total"
echo ""

printf "  ${BOLD}Status HTTP${RESET}\n"
for status in $(echo "${!status_map[@]}" | tr ' ' '\n' | sort); do
    count=${status_map[$status]}
    pct=$(awk "BEGIN{printf \"%.1f\", ($count / $total) * 100}")
    if [[ "$status" == "2"* ]]; then
        printf "    ${GREEN}%-8s${RESET}  %3d  (${GREEN}%s%%${RESET})\n" "$status" "$count" "$pct"
    else
        printf "    ${RED}%-8s${RESET}  %3d  (${RED}%s%%${RESET})\n" "$status" "$count" "$pct"
    fi
done

if [ "$ok_count" -gt 0 ]; then
    avg_score=$(awk "BEGIN{printf \"%.4f\", $score_sum / $ok_count}")
    score_min_f=$(printf "%.4f" "$score_min")
    score_max_f=$(printf "%.4f" "$score_max")
    approved_pct=$(awk "BEGIN{printf \"%.1f\", ($approved_count / $ok_count) * 100}")
    rejected_pct=$(awk "BEGIN{printf \"%.1f\", ($rejected_count / $ok_count) * 100}")

    echo ""
    printf "  ${BOLD}Score de Fraude${RESET}  ${DIM}(sobre %d respostas 200)${RESET}\n" "$ok_count"
    printf "    %-26s ${BOLD}%s${RESET}\n"  "Médio:"   "$avg_score"
    printf "    %-26s ${GREEN}%s${RESET}\n" "Mínimo:"  "$score_min_f"
    printf "    %-26s ${RED}%s${RESET}\n"   "Máximo:"  "$score_max_f"

    echo ""
    printf "  ${BOLD}Decisão de Aprovação${RESET}\n"
    printf "    ${GREEN}✓ Aprovadas:${RESET}  %3d  (${GREEN}%s%%${RESET})\n" "$approved_count" "$approved_pct"
    printf "    ${RED}✗ Negadas:  ${RESET}  %3d  (${RED}%s%%${RESET})\n"    "$rejected_count" "$rejected_pct"

    bar_total=40
    bar_ok=$(awk "BEGIN{printf \"%d\", ($approved_count / $ok_count) * $bar_total}")
    bar_no=$((bar_total - bar_ok))
    echo ""
    printf "    ${GREEN}"
    printf '█%.0s' $(seq 1 $bar_ok)
    printf "${RED}"
    printf '░%.0s' $(seq 1 $bar_no)
    printf "${RESET}  %s%%\n" "$approved_pct"
fi

# ── Estatísticas de Desempenho ────────────────────────────────────────────────

echo ""
printf "  ${BOLD}${MAGENTA}Desempenho${RESET}\n"

avg_req_ms=$(awk "BEGIN{printf \"%.1f\", ($time_sum_s / $total) * 1000}")
time_min_ms=$(awk "BEGIN{printf \"%.0f\", $time_min_s * 1000}")
time_max_ms=$(awk "BEGIN{printf \"%.0f\", $time_max_s * 1000}")
throughput=$(awk "BEGIN{printf \"%.2f\", $total / ($total_wall_s + 0.001)}")

printf "    %-26s ${BOLD}%s ms${RESET}\n"     "Tempo médio/req:"  "$avg_req_ms"
printf "    %-26s ${GREEN}%s ms${RESET}\n"    "Tempo mínimo:"     "$time_min_ms"
printf "    %-26s ${RED}%s ms${RESET}\n"      "Tempo máximo:"     "$time_max_ms"
printf "    %-26s ${BOLD}%s req/s${RESET}\n"  "Throughput:"       "$throughput"
printf "    %-26s ${CYAN}%s s${RESET}\n"      "Tempo total:"      "$total_wall_s"

echo ""
printf "${DIM}══════════════════════════════════════════════════════${RESET}\n"
echo ""
