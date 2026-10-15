# Praca nad platformctl

Flow jest adaptacją procesu MathCore do CLI i dwóch repozytoriów. Operacyjne kontrole dotyczą konkretnej pary CLI i szablonu, nie deploymentu aplikacji webowej.

## Zadanie i status

Przeczytaj Jira, kryteria, zależności i AGENTS.md. Potwierdzone statusy PLAT: Do zrobienia → W toku → Weryfikacja → Gotowe. Blokadę oznacz etykietą blocked i komentarzem z warunkiem odblokowania; przestrzeń nie ma osobnego statusu Blokada.

Jedno zadanie, jeden chat, gałąź c/<klucz>-opis i PR. Przy zmianie obu repozytoriów dwa PR powiązane tym samym zadaniem, z kolejnością i macierzą zgodności. Przed zmianą sprawdź aktualny main oraz cudze zmiany. Finalną synchronizację i dostarczenie serializuj.

## Weryfikacja i review

Podstawowa walidacja CLI:

```sh
go vet ./...
go test ./...
go test -race ./...
go build -o /tmp/platformctl-check .
```

Dołącz testy zachowania adekwatne do zadania: pliki i stan przed/po anulowaniu, niezmieniony i zmieniony plan, przerwanie i resume, dwa procesy, niewłaściwy kube-context, awarie sieci i zależności. Nie deklaruj Kind/AWS E2E na podstawie fixture echo.

Szablony sprawdzaj CLI z jawnego SHA, z izolowanym workspace i kubeconfig. Zapisz wersje narzędzi/chartów, wynik renderowania i readiness, sprawdź brak własnych zasobów po destroy. Cloud wymaga jawnego zakresu i budżetu.

Niezależny reviewer sprawdza konkretne 40-znakowe HEAD SHA, zadanie, testy, ochronę state i danych, zgodność szablonów oraz rollback. Blokujące ustalenia wymagają CHANGES_REQUESTED, poprawki i kolejnego review nowego SHA. Nie zastępuj niezależnego review samooceną.

Zapis rzeczywistego review w komentarzu PR:

```text
<!-- platformctl-review:v1 -->
Commit: <pełny SHA>
Implementer: <tożsamość>
Reviewer: <niezależna tożsamość>
Verdict: APPROVED / CHANGES_REQUESTED
Blocking findings: <liczba>
Zakres i dowody: <wynik>
Ograniczenia: <niewykonane kontrole>
```

## Merge i wydanie

Wymagane: aktualny main, review i SUCCESS CI dokładnego HEAD. Nowy commit wymaga nowego review i CI. Nie obchodź kontroli. Po merge sprawdź wynikowe SHA na main i użyj go do artefaktu oraz końcowej weryfikacji.

CI wymaga process-policy, quality i template-contract. Quality sprawdza format, vet, unit, race, build i testy procesu. Template-contract waliduje i renderuje manifesty bez wykonywania kroków. Process-policy sprawdza tytuł, gałąź, opis PR i nazwy commitów. Wymagany jest SUCCESS każdej kontroli.

Merge wykonuj z aktualnego checkoutu:

```sh
python3 scripts/merge_pr.py --repo GCICHON-DEV/platformctl --pr NUMER --approved-head PELNY_SHA
python3 scripts/merge_pr.py --repo GCICHON-DEV/platformctl --pr NUMER --approved-head PELNY_SHA --merge
```

Pierwsza komenda tylko sprawdza. Skrypt akceptuje wyłącznie oba repozytoria platformctl, wymaga aktualnego main, bieżącego HEAD, sukcesu wymaganych kontroli i najnowszego zatwierdzenia dokładnego SHA. Komentarz review publikuje właściciel GCICHON-DEV na podstawie faktycznego raportu niezależnego reviewera; wpisuje różne tożsamości Implementer/Reviewer. Jest to atestacja właściciela, a nie niezależne uwierzytelnienie konta agenta przez GitHub. Formalne CHANGES_REQUESTED również blokuje. Nie używaj bezpośredniego merge omijającego skrypt.

Ochrona main wymaga strict checks, włącza egzekwowanie dla administratora i blokuje force push/deletion. Samo natywne ustawienie GitHuba nie weryfikuje tożsamości agentów w komentarzu — ten warunek sprawdza skrypt i rzeczywiste review.

Dystrybucja binarek, pełna macierz wersji CLI–template i prawdziwe Kind E2E pozostają w backlogu. Bazowa walidacja kontraktu nie jest testem działającej platformy.

Dla docs deploy nie dotyczy. Dla zmiany CLI wymagany jest build/instalacja z właściwego SHA i scenariusz zadania. Dla zmiany szablonu wymagane jest E2E na izolowanym środowisku odpowiednim do zadania. Sam platformctl status pokazuje obecnie zapis lokalny, nie stan klastra.

Rollback: zachowaj poprzednią binarkę, pin szablonu i zgodny state. Zmiana wersji nie odwraca infrastruktury. Destrukcyjna naprawa/restore wymaga osobnego zakresu; przy regresji pozostaw zadanie otwarte.

## Raport

Chat i Jira otrzymują start, istotne zmiany stanu i raport końcowy; nie wysyłaj wiadomości do innych osób. Szablon raportu:

```text
Zadanie i stan Jira:
Efekt:
Testy / ograniczenia:
Review / PR / sprawdzony SHA:
Merge / wynikowy SHA:
Wydanie lub instalacja / CLI SHA / template SHA:
Weryfikacja końcowa / readiness / zachowanie danych:
Rollback:
Sprzątanie / zachowane materiały:
Dalej / checkout i gałąź przy przerwaniu:
```

Nie oznaczaj Gotowe przed kryteriami akceptacji. queued/pending/skipped nie oznaczają sukcesu. Przy niewykonanym etapie podaj powód; dla etapu niepotrzebnego nie dotyczy z uzasadnieniem.

## Sprzątanie

Zachowaj dowody w PR/CI bez sekretów. Usuń tylko własne odtwarzalne pliki. Chroń .env, kubeconfig, Terraform state, lockfiles, state.json, dane i cudze worktree. Bez globalnego prune i git clean -fdx. Archiwizacja zarządzanego worktree wymaga potwierdzonego wyniku; przy niedokończonym zadaniu zachowaj checkout.
