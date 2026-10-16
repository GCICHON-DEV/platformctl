# Instrukcja projektu dla pilota Symphony

Ten plik jest odczytywany jako instrukcja projektu. Nie uruchamia daemonu
i nie jest natywną, pełną konfiguracją Symphony. Issue z Jira pobiera workflow
hosta; kontroler przydziela Jira issue ID oraz claim. Nie wybieraj kolejnego
zadania ani nie przejmuj claimu samodzielnie.

- Przeczytaj [AGENTS.md](AGENTS.md), [CONTRIBUTING.md](CONTRIBUTING.md), całe
  przydzielone issue i aktualne skrypty. Jedno issue → jedna gałąź → jeden PR
  na repozytorium; pracuj tylko lokalnie w przydzielonym checkoutcie.
- Czytaj przypięty checkout templates w `/templates` bez zmian. Dla PLAT-45
  baza CLI to `51da2453df12f4945c7544d53d29b189c68449f6`, templates to
  `fa43e1ac2c6f07b62fdfb0c265489097ea0a17e9`. Raportuj sprawdzoną parę;
  pin nie jest dowodem Kind/AWS E2E.
- Wykonaj adekwatne testy CLI: `go vet ./...`, `go test ./...`,
  `go test -race ./...`, build do `/tmp` i `go run ./tools/check-templates`.
  Dokumentację sprawdź pod kątem linków i zgodności z kodem. Korzystaj tylko
  z przygotowanych zależności; nie instaluj ani nie pobieraj narzędzi.
- Przy blokadzie raportuj dokładną komendę, wynik, zachowane materiały
  i warunek odblokowania. Niewykonana kontrola nie jest sukcesem. Chroń
  sekrety, stan, dane operatora i cudze zmiany; nie zmieniaj sandboxu.
- W pilocie host prowadzi niezależne review dokładnego SHA, CI i publikację
  PR. Nowy commit unieważnia review i CI; obowiązują bramki z CONTRIBUTING.
  Nie uruchamiaj innych agentów, nie commituj, nie pushuj ani nie publikuj.
- Brak produkcji: bez merge, deploy, instalacji CLI operatora, Kind/AWS
  apply ani destroy. Zostaw lokalne pliki i raport dla hosta, zakończ turn
  dla zapisania pauzy przez after hook; nie zamykaj Jira jako Gotowe.

Przebieg operatora i wznowienie opisuje [docs/symphony.md](docs/symphony.md).
