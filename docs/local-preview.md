# Lokalny preview `custom-minimal` bez chmury

Przykład wymaga powłoki POSIX, Git, standardowych narzędzi systemowych oraz
**już zbudowanej** binarki CLI. Używa wyłącznie syntetycznej nazwy projektu
i lokalnego szablonu; nie wymaga sieci, credentials, Dockera, klastra ani
narzędzi infrastruktury i nie tworzy płatnych zasobów.

Sprawdzona baza CLI i szablonu `custom-minimal` to
`77add03920e25dbb12341a68712a8fdd4f43d8e7`. Szablon znajduje się w
[`examples/local-templates/custom-minimal`](../examples/local-templates/custom-minimal/platform.template.yaml)
w repozytorium CLI. Osobny checkout szablonów infrastruktury `/templates`
ma pin `fa43e1ac2c6f07b62fdfb0c265489097ea0a17e9`; ten przykład go nie używa.
Lokalne `template.source` wskazuje katalog z `platform.template.yaml`
i `files/`; pole `version` nie przypina zawartości lokalnego katalogu.
Dlatego użyj istniejącego, niezmodyfikowanego checkoutu wskazanego SHA.

## Wykonanie

Ustaw dwie ścieżki absolutne do przygotowanych lokalnie materiałów.
Poniżej checkout jest w `/workspace`, a binarka w `/tmp/platformctl-check`.
Blok wykonaj w jednej powłoce; nie buduje ani nie pobiera niczego:

```sh
set -eu
preview_repo=/workspace
preview_cli=/tmp/platformctl-check
test -x "$preview_cli"
test "$(git -C "$preview_repo" rev-parse HEAD)" = 77add03920e25dbb12341a68712a8fdd4f43d8e7
git -C "$preview_repo" diff --exit-code HEAD -- examples/local-templates/custom-minimal

preview_dir=$(mktemp -d /tmp/platformctl-preview.XXXXXX)
mkdir "$preview_dir/home" "$preview_dir/work"
cd "$preview_dir/work"
cat > platform.yaml <<EOF
template:
  source: "$preview_repo/examples/local-templates/custom-minimal"
values:
  project_name: preview-demo
EOF

# Czyste środowisko i pusty HOME tylko dla procesu CLI.
preview() {
  env -i HOME="$preview_dir/home" PATH=/usr/bin:/bin "$preview_cli" "$@"
}
preview status
test ! -e generated
test ! -e .platformctl

preview plan
cat generated/custom/README.md
preview status
test -f .platformctl/state.json
test -f .platformctl/lock
printf 'Materiały preview: %s\n' "$preview_dir"
```

## Oczekiwany wynik i granice preview

- Po ręcznym zapisie konfiguracji istnieje tylko `platform.yaml` w katalogu
  roboczym. Pierwszy `status` wypisuje `No local platform state found.`
  i nie tworzy `generated/` ani `.platformctl/`.
- `plan` wypisuje `CLI workflow preview (not a Terraform saved plan).`,
  digest, plik wynikowy, wymaganie `echo` oraz kroki plan/apply/destroy
  z wyrenderowanymi argumentami. Tworzy `generated/custom/README.md`
  z nagłówkiem `# preview-demo` oraz `.platformctl/state.json` i plik
  blokady `.platformctl/lock`. Pozostający plik blokady nie oznacza,
  że proces nadal działa; nie usuwaj go podczas pracy CLI.
- Drugi `status` odczytuje zapis lokalny: źródło szablonu, checksum,
  hashe, `Last phase: plan` i `Managed files: 1`. Nie sprawdza stanu klastra
  ani gotowości infrastruktury i nie zmienia plików. Nie ma wykonanych kroków
  ani zatwierdzenia wykonania. Materiały pozostają w wypisanym katalogu `/tmp`.

`plan` zapisuje renderowany preview, ale nie instaluje narzędzi, nie sprawdza
credentials i nie wykonuje nawet `echo` z `steps.plan`. Pokazane polecenia
workflow są opisem przyszłych kroków. Nie uruchamia Terraform, nie odczytuje
stanu zdalnych zasobów i nie tworzy zapisanego `terraform plan`.
Także `plan --json` wykonuje renderowanie i zapis stanu; format JSON
nie oznacza trybu bez zapisu. Digest zależy m.in. od ścieżki workspace,
wartości i lokalnej konfiguracji, więc nie oczekuj stałego hasha między
katalogami. Przykład kończy się na lokalnym renderowaniu i odczycie stanu.
