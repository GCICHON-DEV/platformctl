# Wyjście CLI i kody zakończenia

`--json` zwraca jeden dokument JSON na stdout: wynik albo błąd. Nie łącz
stdout i stderr przed parsowaniem. Stderr zawiera wyjście kroków workflow
(oba strumienie procesu potomnego), komunikaty toolchain i diagnostykę
niemożności zapisania wyniku. Brak komunikatów na stderr nie dowodzi sukcesu;
sprawdź kod zakończenia oraz wynik kontroli w JSON.

```sh
platformctl preflight --json >result.json 2>workflow.log
code=$?
# code=0 może oznaczać zakończony preflight z ok=false; sprawdź result.json.
```

## Wyniki

Poniższe pola najwyższego poziomu zachowują dotychczasowy kontrakt.
Tablice mogą być `null`, gdy wynik nie ma elementów. Nie zakładaj kolejności
pól ani stałych białych znaków. Pola z `omitempty` mogą być nieobecne.

| Polecenie | Dokument stdout z `--json` | Bez `--json`: stdout |
| --- | --- | --- |
| `init` | `ok: bool`, `config: string`, `template: string`, `preflight: object` | Utworzona konfiguracja i wynik preflight. |
| `preflight`, `doctor` | `ok: bool`, `template: string`, `source: string`; opcjonalne `checksum`, `tools`, `credentials`, `warnings`, `checks` | Kontrole zależności, credentials i gotowości lokalnej. |
| `plan` | `ok: true`, `plan: object` | Podgląd workflow, ostrzeżenia i ścieżka zapisanego stanu. |
| `apply`, `up` | `ok: true`, `phase: "apply"`, `generated_files`, `success_messages`, `notes`, `next_steps`, `warnings` | Podgląd, potwierdzenie, postęp, wyjście kroków i wynik. |
| `destroy`, `down` | `ok: true`, `phase: "destroy"`, `generated_files`, `warnings` | Podgląd, potwierdzenie, postęp, wyjście kroków i wynik. |
| `status` | `ok: true`, `state: object` | Lokalny zapis stanu lub informacja o jego braku. |
| Root bez polecenia, `help [command]`, `--help`, `-h` | `ok: true`, `help: string` | Pomoc tekstowa. |

`preflight` wewnątrz wyniku `init` ma taki sam format jak osobne polecenie.
`tools` zawiera dotychczasowe statusy narzędzi; `credentials` i `checks` mają
`name`, `ok` oraz opcjonalne `message`. `warnings` jest tablicą napisów.
`plan` i `state` zachowują format z [modelu planu](../internal/templateengine/engine.go)
i [modelu stanu](../internal/state/state.go). `status` nie sprawdza klastra.
JSON nie zmienia skutków polecenia: `plan` nadal renderuje pliki i zapisuje stan,
a `init` może instalować narzędzia wymagane przez wybrany szablon.

## Błędy i zakończenie

Każdy obsłużony błąd w trybie JSON ma format:

```json
{
  "ok": false,
  "error": {
    "code": "PLATFORMCTL_CONFIG_LOAD",
    "category": "configuration",
    "message": "could not load platform.yaml"
  }
}
```

Opcjonalne `context` i `remediation` pozostają napisami. Kategorie:
`configuration`, `dependency`, `execution`, `state`, `template`, `internal`.
Wewnętrzny `Err` nie jest serializowany; `--verbose` nie dodaje przyczyny do JSON.
Bez `--json` błędy trafiają na stderr jako `Error [CODE]: message`, z dostępnymi
wierszami `Context:`, `Fix:` i przy `--verbose` — `Cause:`.

| Kod | Znaczenie |
| --- | --- |
| `0` | Polecenie zakończone. Obejmuje pomoc, odmowę `n` w trybie interaktywnym oraz nieblokujący preflight/init z `ok=false`. |
| `1` | Błąd operacji, zależności, konfiguracji, stanu, szablonu albo serializacji/zapisu wyjścia. Obejmuje EOF podczas potwierdzenia oraz brak `--yes` przy JSON apply/destroy. |
| `2` | Błąd użycia: nieznane polecenie/temat pomocy, nieznana flaga, błędna wartość flagi, brak jej wartości lub nieoczekiwany argument pozycyjny. JSON zawiera `PLATFORMCTL_USAGE` w kategorii `configuration`. |

`preflight --strict` kończy się kodem `1`, gdy kontrola nie przechodzi.
Nieblokujący preflight/init może mieć `ok=false` i kod `0`; konsumenci muszą
odczytać statusy. `--quiet` nie usuwa żądanego JSON ani błędów.

`--json` działa przed i po nazwie polecenia. `--json=false` wyłącza ten tryb.
Ostatnie wystąpienie flagi wybiera format także przy błędzie parsowania.
`--` kończy odczytywanie flag; `status -- --json` nie włącza JSON.
Napis `--json` użyty jako wartość poprawnej flagi, np. `init --project --json`,
również nie wybiera formatu. Walidację poleceń i flag nadal wykonuje Cobra.

Odmowa potwierdzenia, EOF i brak `--yes` nie zmieniają wygenerowanych plików
ani stanu i nie inicjalizują toolchain. Plik blokady workspace może powstać lub
otrzymać nowe metadane diagnostyczne. JSON apply/destroy nie czyta interaktywnego
potwierdzenia; wymaga jawnego `--yes` i zwraca dotychczasowy
`PLATFORMCTL_CONFIRMATION_REQUIRED`, jeśli zatwierdzony plan pozwala dojść do
etapu potwierdzenia. Wcześniejsza walidacja może zwrócić własny błąd.

## Migracja i granice gwarancji

Błędy JSON przeniesiono ze stderr na stdout, aby spełnić kontrakt jednego
dokumentu także przy porażce. Parsery czytające błędy ze stderr muszą odtąd
czytać stdout; stderr pozostaje osobnym logiem. W repozytorium nie znaleziono
takiego parsera; istniejące testy procesów, przykłady i `status --json` zachowują
pola. Nieoczekiwane argumenty, wcześniej ignorowane, są teraz błędem użycia.
Kod `1` błędów operacyjnych i kod `0` odmowy użytkownika pozostają zgodne;
błędy użycia otrzymały kod `2` zamiast wspólnego `1`.

JSON jest kodowany w pamięci przed publikacją. Błąd kodowania nie pozostawia
częściowego sukcesu przed dokumentem błędu. `PLATFORMCTL_OUTPUT_ENCODE` oznacza
problem serializacji, a `PLATFORMCTL_OUTPUT_WRITE` problem zapisu. Nieudany lub
krótki zapis nie może zwrócić kodu `0`. Błąd końcowej publikacji jest raportowany
na stderr; stdout nie jest ponawiany, bo mógł już zawierać część dokumentu.

Fizycznie niezapisywalny stdout, zamknięta rura/SIGPIPE, przerwanie procesu
sygnałem, SIGKILL i awaria systemu mogą uniemożliwić dostarczenie pełnego JSON.
Gwarancja dotyczy obsłużonych wyników przy działającym kanale wyjścia. Ukończona
operacja może już mieć zapisane pliki, stan lub wykonane kroki, gdy publikacja
wyniku zawiedzie; błąd wyjścia nie cofa tych skutków. Sprawdź stan przed ponowieniem.
Wyjście/JSON może zawierać dane ze stanu i zaufanego szablonu; nie jest bezpiecznym
pakietem wsparcia ani automatyczną redakcją sekretów.

Test `TestJSONBinaryContract` buduje rzeczywiste main i parsuje dokładnie jeden
dokument dla sukcesu, ostrzeżeń, porażek, pomocy i odmowy bez `--yes`. Sprawdza
także osobne strumienie kroków, anulowanie/EOF bez zmian danych, kod `2` oraz
niezapisywalny stdout. Testy granicy procesu obejmują błędy kodowania i krótki
zapis. Nie używają chmury, klastra ani globalnych instalacji.
