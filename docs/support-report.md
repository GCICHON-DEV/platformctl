# Bezpieczne zgłoszenie błędu CLI

Zacznij od [troubleshooting](troubleshooting.md). Skopiuj poniższy formularz
i uzupełnij tylko informacje potrzebne do odtworzenia problemu.

## Formularz

```text
Tytuł: <polecenie i krótki opis objawu>
CLI: <wersja/identyfikator artefaktu; pełny SHA źródeł, jeśli znany>
System: <OS, architektura, powłoka; wersje narzędzi związanych z błędem>
Szablon: <template.source oraz template.version lub pin source@ref>
Pin: <pełny SHA repozytorium szablonów; lokalne zmiany: tak/nie/nieznane>
Checksum: <jeśli dostępny; dla lokalnego szablonu także minimalne pliki źródłowe>
Reprodukcja: <syntetyczne pliki, katalog roboczy i dokładna kolejność poleceń>
Oczekiwany wynik: <konkretne zachowanie>
Faktyczny wynik: <objaw i krótki, zredagowany fragment stdout/stderr>
Błąd: <PLATFORMCTL_… oraz Context/Fix, jeśli występują; inaczej brak>
Status zakończenia procesu: <liczba; osobno od kodu PLATFORMCTL_…>
Stan lokalny: <brak/istnieje; ostatnia faza i istotne ukończone kroki>
Częstotliwość: <zawsze/czasami; ostatnia działająca para CLI–szablony>
Redakcja: <co zastąpiono i czy syntetyczna reprodukcja nadal daje ten sam błąd>
```

Bieżący CLI nie udostępnia komendy `version` ani globalnego `--version`.
Identyfikator CLI weź z pochodzenia używanej binarki/builda; jeśli go nie znasz,
napisz „nieznany”. `init --version` wybiera **wersję szablonu**, nie CLI.
Przepisz pin z używanego `platform.yaml`; nie aktualizuj go podczas diagnozy.
Checksum manifestu nie zastępuje SHA repozytorium ani zawartości lokalnych plików.

## Minimalna syntetyczna reprodukcja

Użyj nowego, pustego katalogu i fikcyjnych wartości. Dołącz najmniejszy
`platform.yaml` i lokalny szablon potrzebne do wywołania objawu. Przykładowy
punkt wyjścia bez poświadczeń; `plan` tylko renderuje pliki i opis kroków:

```yaml
# platform.yaml
template:
  source: ./template.yaml
values: {}
```

```yaml
# template.yaml
apiVersion: platformctl.io/v1beta1
kind: PlatformTemplate
metadata:
  name: support-demo
  description: Synthetic rendering example.
files:
  - path: generated/example.txt
    content: "synthetic example"
steps:
  apply:
    - {name: synthetic apply, command: echo, args: [demo]}
  destroy:
    - {name: synthetic destroy, command: echo, args: [demo]}
```

W tym katalogu uruchom w powłoce POSIX:

```sh
platformctl plan --json
printf 'exit=%s\n' "$?"
platformctl status
```

Oczekiwany wynik tego przykładu to status procesu 0, plik
`generated/example.txt` i ostatnia faza `plan`. Dostosuj pliki do błędu;
jeśli przykład go nie odtwarza, zaznacz to. `plan` zapisuje pliki i lokalny stan,
zeruje checkpointy oraz zatwierdzenie; nie wykonuj go w oryginalnym workspace
tylko po to, by zebrać raport. `status` pokazuje zapis lokalny, nie stan klastra.
Nie uruchamiaj `apply` ani `destroy` w celu zebrania diagnostyki.

## Przed wysłaniem

Nie przesyłaj pełnego env, `.env`, `.platformctl/state.json`, Terraform state,
kubeconfig ani plików credentials. Wybierz ręcznie tylko istotne pola stanu;
`status --json` zwraca cały zapis stanu i nie jest bezpiecznym pakietem wsparcia.
Zmienne środowiskowe opisz przez nazwę i ustawiona/nieustawiona; wartości zastąp
fikcyjnymi. Usuń tokeny, hasła, klucze, nagłówki autoryzacji, dane kont,
prywatne adresy i ścieżki także z poleceń, URL-i, szablonów oraz logów.
Stosuj konsekwentne znaczniki, np. `<REDACTED_TOKEN>` i `demo-cluster`.
JSON i `--verbose` nie anonimizują danych; obejrzyj każdy fragment przed wysłaniem.
Zachowaj oryginalny stan lokalnie, bez edycji hashy, usuwania blokad lub danych.
