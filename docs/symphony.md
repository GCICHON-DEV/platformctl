# Pilot Symphony dla platformctl

PLAT-45 obejmuje odbiór procesu; adaptacja tego repozytorium dostarcza
[instrukcję projektu](../WORKFLOW.md) i ten opis operatora. Hostowy workflow
pobiera issue z Jira i odczytuje instrukcje repo. Sam `WORKFLOW.md` nie
uruchamia daemonu i nie jest natywną, pełną konfiguracją Symphony.

Runtime, auth, claim i kolejka należą do repo `dev-infrastucture`.
Źródłem procedury jest tam `docs/symphony-rollout-plan.md` (2026-10-16).
Nie kopiujemy tej konfiguracji do platformctl. Pilot jest ręczny: nie
deklaruje automatycznego skanowania Desktop ani gotowej globalnej kolejki.
Nie uruchamia pozostałego backlogu.

## Od dopuszczenia do lokalnej implementacji

1. Operator jawnie dopuszcza konkretne issue jako `agent-ready` i sprawdza
   kryteria, zależność S3 oraz brak kolizji z PLAT-13. Sama etykieta nie
   zastępuje admission ani nie uruchamia pracy. Kontroler przydziela issue
   ID i claim; obowiązuje jedno issue, jedna gałąź i jeden PR na repozytorium.
2. Operator korzysta w repo kontrolera z ręcznych operacji `pilot.py prepare`
   oraz `pilot.py prepare-deps`, zgodnie z jego aktualną dokumentacją.
   Przygotowuje izolowany katalog CLI, przypięte templates do odczytu
   i zależności. Admission jest jawną decyzją operatora z użyciem
   `jobs.py authorize`. Przed startem operator sprawdza bieżący SHA CLI,
   pin templates, zakres dopuszczenia i zgodność z istniejącym jobem/claimem.
3. `pilot.py start` może wykonać wyłącznie operator po sprawdzeniu aktualnego
   SHA. Implementer nie uruchamia kontrolera. Skryptów `pilot.py` i `jobs.py`
   nie ma w tym repo ani w udostępnionym checkoutcie; wymienione nazwy operacji
   pochodzą z zakresu pilota. Argumenty i szczegóły admission należy sprawdzić
   w `dev-infrastucture`; nie podajemy niezweryfikowanej składni wywołania.
4. Implementer czyta [AGENTS.md](../AGENTS.md),
   [CONTRIBUTING.md](../CONTRIBUTING.md), issue i skrypty. Pracuje lokalnie
   na przydzielonej gałęzi, chroni dane i raportuje blokady wraz z warunkiem
   odblokowania. Dla tej adaptacji zmienia wyłącznie dwa nowe pliki:
   `WORKFLOW.md` oraz `docs/symphony.md`. Nie commituje ani nie publikuje PR;
   przekazuje zwykłe pliki hostowi.

## Walidacja i przekazanie do PR

W przygotowanym środowisku, bez instalowania ani pobierania zależności:

```sh
go vet ./...
go test ./...
go test -race ./...
go build -o /tmp/platformctl-pilot .
go run ./tools/check-templates
```

Sprawdź lokalne linki w obu dokumentach i opisy komend względem aktualnych
skryptów. W nowym syntetycznym pustym katalogu uruchom
`/tmp/platformctl-pilot status --json` i zachowaj wynik poza repo bez danych
operatora. Ta komenda pokazuje lokalny zapis stanu; nie sprawdza klastra.
Testy z błędem raportuj z dokładnym wynikiem zamiast deklarować sukces.

Host przejmuje lokalne zmiany, przygotowuje commit i koordynuje niezależnego
reviewera dla dokładnego pełnego SHA oraz publikację PR i CI. Opis PR musi
spełnić [szablon](../.github/pull_request_template.md) i
[process-policy](../scripts/process_policy.py). Wymagane kontrole to
`process-policy`, `quality`, `template-contract`, każda z wynikiem SUCCESS.
Nowy commit, także po poprawkach lub synchronizacji main, wymaga nowego review
i CI. Rzeczywiste niezależne APPROVED, brak blokujących ustaleń i aktualna
baza pozostają wymagane zgodnie z CONTRIBUTING oraz
[bramką dostarczenia](../scripts/merge_pr.py). Pilot kończy się na PR;
nie upoważnia do merge ani obejścia istniejących bramek.

Implementer przekazuje raport z plikami, testami, ograniczeniami, bazowym SHA
i parą CLI/templates, po czym kończy turn. Hostowy after hook zapisuje trwałą
pauzę. Host potwierdza stan przez `jobs.py status` i zachowuje katalog, gałąź,
powiązanie issue/claim, PR i dowody walidacji w stanie kontrolera. Sam koniec
turnu ani otwarty PR nie jest dowodem zapisu pauzy lub ukończenia zadania.
Bez potwierdzenia stanu należy zgłosić blokadę. Jira pozostaje otwarta;
lokalna dokumentacja nie potwierdza odbioru całego PLAT-45.

## Jawne wznowienie i anulowanie

Pauza trwa do jawnej decyzji operatora; restart hosta nie oznacza zgody na
dalszą pracę. Operator najpierw sprawdza `jobs.py status`, istniejący katalog,
gałąź, PR, aktualny HEAD i parę CLI/templates oraz brak konkurencyjnej pracy.
Wznowienie wykonuje przez `jobs.py resume` według dokumentacji kontrolera,
z zachowaniem istniejącego katalogu i PR. Nie tworzy drugiej dostawy tego
samego issue. Jeśli potrzebny jest start procesu, `pilot.py start` nadal
wykonuje wyłącznie operator po ponownym sprawdzeniu aktualnego SHA.
Brak katalogu, rozbieżność SHA albo niejasny claim wymagają wyjaśnienia
i raportu blokady przed kontynuacją.

Wycofanie dopuszczenia obsługuje operator przez `jobs.py revoke`, a wynik
sprawdza przez `jobs.py status`. Samo revoke nie zatrzymuje procesu.
Operator osobno zatrzymuje własny profil kontrolerem i potwierdza zakończenie
procesu oraz kontenera; `stage=paused` nie jest dowodem ich zatrzymania.
Niepewny wynik stop wymaga uzgodnienia przed przejęciem lub resume.
Anulowanie nie oznacza usunięcia katalogu,
PR, stanu lub danych ani wykonania destroy. Zachowaj materiały do audytu
i ewentualnego jawnego resume. Test restartu i anulowania wymaga dowodów
z kontrolera; sam opis tego przebiegu ich nie dostarcza.

## Kontrola pary CLI/templates i granice dowodów

Dla tego pilota przypięta para to baza CLI
`51da2453df12f4945c7544d53d29b189c68449f6` oraz checkout `/templates`
`fa43e1ac2c6f07b62fdfb0c265489097ea0a17e9`. Operator sprawdza rzeczywiste
SHA przy przygotowaniu, przed startem i przy resume. Host zapisuje też nowe
SHA CLI użyte do review i CI; baza nie zastępuje identyfikatora ocenianej
zmiany. Zmiana któregoś pinu wymaga ponownego sprawdzenia zgodności pary.

[check-templates](../tools/check-templates/main.go) domyślnie waliduje
i renderuje `examples/template-registry`, bez wykonywania kroków workflow.
Obsługuje również katalog manifestów jako argument, więc
`go run ./tools/check-templates /templates` pozwala lokalnie sprawdzić
przypięty checkout. Wynik domyślnego polecenia nie dowodzi sprawdzenia
zewnętrznych templates. Ani pin, ani kontrakt, ani `status --json` nie
potwierdzają Kind/AWS E2E, readiness czy wdrożenia.

Pilot nie obejmuje produkcji, merge, push, deploy, instalacji CLI operatora,
Kind/AWS apply ani destroy. Nie pobieraj sekretów, nie zmieniaj sandboxu
i nie uruchamiaj innych agentów. Sieć komend jest wyłączona; używaj
przygotowanego cache Go tylko do odczytu. Dla dokumentacji deploy nie dotyczy.
Review, CI, publikacja PR, kontrola restartu/anulowania i potwierdzenie pauzy
pozostają po stronie hosta.
