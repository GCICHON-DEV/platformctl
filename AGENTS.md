# platformctl — zasady pracy

Przeczytaj CONTRIBUTING.md. Rozmowa, Jira i opisy PR domyślnie po polsku.
Zakres obejmuje platformctl oraz platformctl-templates; nie zmieniaj MathCore.

## Realizacja

- Jedno zadanie Jira → jeden chat → jedna gałąź → jeden PR na repozytorium. Nazwa chatu: klucz i dokładny tytuł Jira. Zadanie obejmujące oba repozytoria ma dwa powiązane PR i opis kolejności wydania.
- Odczytaj całe zadanie, kryteria i zależności. Sprawdź git status i aktualny main. Nie nadpisuj cudzych plików. Dla pracy równoległej użyj osobnego worktree; gałąź c/<klucz>-opis.
- Przed implementacją podaj zakres, plan i etap; zapisz komentarz startowy Jira. Rutynowe decyzje podejmuj samodzielnie.
- Zlecenie implementacji domyślnie obejmuje testy, PR, review, CI, merge, instalację lub wydanie w istniejącym środowisku testowym i weryfikację. Respektuj ograniczenie do audytu lub PR. Planowanie backlogu nie jest zleceniem implementacji wszystkich zadań.
- Nie uruchamiaj kosztownego cloud apply, destroy istniejących zasobów ani restore danych bez jawnego zakresu obejmującego konto, region, zasoby i budżet. Nie korzystaj z osobistych klastrów jako fixture. Lokalne E2E używa osobnego klastra i kubeconfig.
- Zachowuj state.json, Terraform state/lock, kubeconfig, sekrety i dane. Przed zmianą formatu stanu określ migrację i rollback. Nie wykonuj globalnego prune ani git clean -fdx.

## Dostarczenie

- Dla implementacji zleć niezależne review osobnemu agentowi przez narzędzia współpracy; to jest jawne upoważnienie do delegowania review. Reviewer otrzymuje zadanie, bazę, pełny HEAD SHA i wyniki testów; nie implementuje poprawek.
- Nowy commit, także synchronizacja main, unieważnia poprzednie review i CI. Popraw blokujące ustalenia i ponów review dokładnego SHA.
- Merge tylko po aktualizacji do main, rzeczywistym niezależnym APPROVED i SUCCESS wszystkich wymaganych kontroli. Nie używaj --admin ani pomijania bramek.
- Finalną synchronizację, review, merge i wydanie serializuj między zadaniami. Zmiany CLI i szablonów weryfikuj jako konkretną parę SHA/wydania. Nie zakładaj, że lokalna blokada CLI koordynuje inne chaty lub maszyny.
- CI ma wymagane kontrole process-policy, quality i template-contract. Merge wykonuj przez python3 scripts/merge_pr.py --repo GCICHON-DEV/platformctl --pr NUMER --approved-head SHA --merge. Bramka wymaga komentarza z rzeczywistego niezależnego review; release i prawdziwe Kind E2E pozostają w backlogu.
- Po merge sprawdź CI wynikowego SHA. Dla runtime sprawdź artefakt z tego SHA i scenariusze zadania; dla szablonu rzeczywiste renderowanie, apply, readiness i destroy w izolowanym środowisku. Dokumentacja wymaga kontroli linków i zgodności z kodem; deploy oznacz nie dotyczy.
- Przy regresji wycofaj wersję CLI/szablonu, jeśli jest to bezpieczne; cofnięcie binarki nie cofa infrastruktury ani stanu. Nie niszcz zasobów ani nie przywracaj state automatycznie.
- Gotowe dopiero po wszystkich kryteriach zadania i dowodach końcowej weryfikacji. Sam merge, zielone unit tests lub lokalny status nie dowodzą działającej platformy.

## Raport i sprzątanie

- Chat i Jira: start, istotny postęp, blokada, review, CI, merge, weryfikacja i wynik końcowy. Bez powtarzania niezmienionego stanu i bez wiadomości do innych osób.
- Raport końcowy: efekt, testy i niewykonane kontrole, PR/reviewer/SHA, merge, rzeczywiste wydanie/instalacja, para CLI–template, readiness i dane, rollback, sprzątanie, następny krok. Przy przerwaniu podaj checkout i gałąź. queued nie oznacza ukończenia.
- Usuń tylko własne odtwarzalne pliki tymczasowe po zachowaniu dowodów. Zachowaj cudze zmiany, współdzielone cache, dane i konfigurację. Zarządzany worktree archiwizuj po potwierdzonym dostarczeniu; przy przerwaniu zachowaj materiały do kontynuacji.

## Dokumentacja i nazwy

- Context7: resolve-library-id, następnie query-docs przy składni/config/API/migracji i debugowaniu bibliotek, SDK, CLI lub usług cloud. Zwykłe review/logika nie wymaga tego.
- Conventional Commits: type(scope): konkretny efekt; scope np. cli, state, templates, toolchain, ci, docs.
- Dokumentacja krótka, konkretna i zgodna z wykonanymi kontrolami. Opis PR według .github/pull_request_template.md.
