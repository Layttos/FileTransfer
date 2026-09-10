#!/bin/sh
#
# Vérifie l'ensemble du projet avant un déploiement.
#
#   ./tools/check.sh
#
# Enchaîne le formatage et les tests Go, puis les contrôles des pages web. Ces
# derniers comptent particulièrement : le projet n'a aucune étape de build, donc
# rien d'autre ne relit le JavaScript avant qu'il n'arrive chez un visiteur.
#
set -e
cd "$(dirname "$0")/.."

fail=0
step() { printf '\n\033[1m%s\033[0m\n' "$1"; }

step "Formatage Go"
unformatted=$(gofmt -l . 2>/dev/null || true)
if [ -n "$unformatted" ]; then
    echo "  fichiers non formatés :"
    echo "$unformatted" | sed 's/^/    /'
    fail=1
else
    echo "  ok"
fi

step "go vet"
if go vet ./... 2>&1 | sed 's/^/  /'; then echo "  ok"; else fail=1; fi

step "Compilation"
if go build -o /dev/null . ; then echo "  ok"; else fail=1; fi

step "Tests Go"
if go test ./... 2>&1 | sed 's/^/  /'; then :; else fail=1; fi

if command -v node >/dev/null 2>&1; then
    step "Exécution des pages"
    if node tools/check-pages.js; then :; else fail=1; fi

    step "Traductions"
    if node tools/check-i18n.js; then :; else fail=1; fi
else
    step "Pages et traductions"
    echo "  ignoré : node n'est pas installé"
fi

printf '\n'
if [ "$fail" -ne 0 ]; then
    printf '\033[31mDes contrôles ont échoué.\033[0m\n'
    exit 1
fi
printf '\033[32mTout est vert.\033[0m\n'
