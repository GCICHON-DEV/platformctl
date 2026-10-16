# Audyt platformctl i plan ukończenia

Data: 2026-10-15. Zadanie procesu: [PLAT-1](https://cichongabriel03.atlassian.net/browse/PLAT-1). [Przestrzeń Jira](https://cichongabriel03.atlassian.net/jira/software/projects/PLAT/boards).

## Zakres i wynik

Analiza CLI i rejestru szablonów: kod wszystkich obszarów cmd/internal, manifesty, testy, dokumentacja, CI, historia wydań i ochrona main. Bazowe SHA: CLI ec7320b7c6c503751aefa996c49e760473964412; rejestr 5da82328723c62e97efe4256f490a232b1dc1c08. Lokalne kopie trzech manifestów są identyczne z rejestrem na tych SHA.

To jest działający prototyp silnika, ale brak dowodu pełnej platformy end-to-end. Unit/race nie zastępują testu Kind/AWS. Nie wykonywano cloud apply, destroy, restore ani operacji na klastrach użytkownika.

## Co już działa

- Manifest v1beta1, źródła registry/HTTPS/local, walidacja inputów, renderowanie, plan kroków.
- Komendy init/preflight/plan/apply/status/destroy, aliasy, JSON, checkpointy i resume.
- Atomowy zapis state, blokada apply/destroy, sumy SHA256 pobranych narzędzi.
- Szablony Kind/EKS i referencyjny AKS; ArgoCD, monitoring i ingress jako kroki instalacji.

## Weryfikacja bazowa

| Kontrola | Wynik |
| --- | --- |
| go test ./... -cover | PASS; cmd 42,0%, templateengine 55,5%, state 68,1%, toolchain 29,0% |
| go vet ./... | PASS |
| go test -race ./... | PASS |
| go build | PASS |
| CI istniejącego main CLI | historyczny run 26242134962 SUCCESS; nie dowodzi E2E |
| Render i walidacja 3 przykładów + 3 manifestów rejestru | PASS przez tools/check-templates; bez instalacji i wykonania kroków |
| Kind / GitOps / HTTP demo | niewykonane |
| AWS / billing / teardown | niewykonane |

Pierwsza próba Go wymagała przeniesienia GOCACHE do /private/tmp ze względu na ograniczenia zapisu środowiska. Nie był to błąd projektu. Pokrycie jest informacją diagnostyczną, nie deklaracją jakości wszystkich ścieżek.

## Najważniejsze luki z kodu

| Obszar | Dowód i konsekwencja | Task |
| --- | --- | --- |
| Zatwierdzony plan | cmd/apply.go i destroy.go generują pliki przed confirm; hash ExecutionPlan nie zawiera treści renderowanych plików. Zmiana treści może ominąć warunek resume. | PLAT-13 |
| Współbieżność | cmd/plan.go zapisuje state/generated bez workflow lock; może kolidować z apply/destroy. | PLAT-12 |
| Integralność plików | GenerateWithPrevious usuwa poprzednie pliki przed ukończeniem renderowania; kolizje i symlinki nie są kompletnie chronione. | PLAT-14, PLAT-15 |
| Zaufanie szablonom | Pobieranie bez oczekiwanego digestu, cichy fallback cache, brak limitu ReadAll. Komenda+args nie tworzą sandboxa dla niezaufanego manifestu. | PLAT-17 |
| Toolchain | Jeden plik binarny i target.tmp per nazwa; projekty z różnymi wersjami mogą kolidować. | PLAT-18 |
| Preflight | Brak timeoutów commandCheck/credential. Apply nie uruchamia pełnego preflight; sam preflight --strict poprawnie zwraca błąd po nieudanych kontrolach lokalnych. | PLAT-20 |
| Logi | stdout/stderr procesów są przekazywane bez redakcji; printSteps drukuje argumenty. | PLAT-19 |
| Stan vs zdrowie | status odczytuje tylko lokalny JSON. Nie potwierdza nodes, aplikacji ani usług. | PLAT-22 |
| Demo | Brak Argo Application/AppProject, demo app i rzeczywistego HTTP testu; brak własnego dashboardu/alertu. | PLAT-27, PLAT-28 |
| Powtarzalność | Charty Helm bez pinów; lokalny ingress wymaga potwierdzenia ścieżki host/NodePort i macierzy wersji. | PLAT-25 |
| AWS | Apply używa -auto-approve bez planfile; brak backendu state, kompletnego cleanup LB/PVC i dowodów kosztowych. | PLAT-31–PLAT-35 |
| Wydanie | Brak GitHub Releases CLI, version/build SHA i LICENSE. Main obu repo bazowo niechronione. | PLAT-36, PLAT-41, PLAT-1 |

Nie jest to pełny audyt bezpieczeństwa ani dowód exploitów. Ryzyka ścieżek i procesu wymagają wskazanych regresji. Bieżąca dokumentacja dostawców musi zostać sprawdzona podczas implementacji pinów i integracji cloud.

## Dwa poziomy ukończenia

Aktualna specyfikacja docelowa: [wymagania v1 i macierz odbioru (PLAT-9)](requirements-v1.md).
Zawiera przegląd driftu względem baz tego audytu, identyfikatory wymagań,
scenariusze odbioru i rejestr niepotwierdzonych wymagań akademickich.
Poniższy audyt zachowuje historyczny stan i wyniki z 2026-10-15.

Portfolio: instalowalne wydanie CLI, bezpieczny lifecycle, prawdziwe lokalne E2E, aplikacja przez GitOps i ingress, monitoring, pokaz awarii/resume, architektura, scenariusz i materiały demo. Wymaga zadań oznaczonych portfolio, w tym wspólnych both. Weryfikacja odbywa się z czystej instalacji; nie wymaga stale działającego płatnego klastra.

Projekt inżynierski: poziom portfolio oraz zweryfikowany EKS z kontrolą danych/kosztów/teardown, formalne wymagania, porównanie z ręcznym bootstrapem, powtarzane pomiary, traceability i opracowanie według wytycznych uczelni. Nie ma jeszcze potwierdzonych wytycznych promotora ani zaakceptowanych wyników eksperymentów.

Azure pozostaje referencyjnym manifestem z walidacją statyczną, poza pierwszą wspieraną wersją.

## Kolejność i bramki

1. PLAT-1: przygotowanie flow i bazowych kontroli. Równolegle możliwa analiza wymagań PLAT-9.
2. Naprawy silnika: plan/lock/pliki/target/trust/cancel, walidacja i toolchain.
3. Powtarzalna platforma lokalna, demo GitOps, monitoring i izolacja.
4. Prawdziwe Kind E2E oraz instalowalne wydanie, następnie odbiór portfolio.
5. Terraform/EKS/state/teardown i kontrolowany odbiór AWS po ustaleniu konta, budżetu i TTL.
6. Eksperymenty porównawcze, praca inżynierska i próba obrony.

Zależności w Jira są blokadami wykonania, nie sprintami ani obietnicami terminów. Finalne review/synchronizacja/merge/wydanie są serializowane. Polityka flow nie oznacza uruchomienia automatycznego harmonogramu wykonywania zadań.

## Backlog

Każdy task ma w Jira opis dowodu, kryteria akceptacji, weryfikację i zależności. Wersja maszynowa: [backlog.json](backlog.json). Etykiety portfolio/engineering rozdzielają odbiory; zadania wspólne mają obie etykiety. 34 zadań, 7 epików; PLAT-1 jest zadaniem przygotowawczym i odpowiada T02.

| Jira | Priorytet | Ścieżka | Zadanie |
| --- | --- | --- | --- |
| [PLAT-9](https://cichongabriel03.atlassian.net/browse/PLAT-9) | High | both | Ustalić wymagania, zakres v1 i macierz odbioru |
| [PLAT-1](https://cichongabriel03.atlassian.net/browse/PLAT-1) | High | both | Wdrożyć flow MathCore i bramki exact-SHA w obu repozytoriach |
| [PLAT-10](https://cichongabriel03.atlassian.net/browse/PLAT-10) | High | both | Rozszerzyć CI CLI o build, format, regresje i artefakty |
| [PLAT-11](https://cichongabriel03.atlassian.net/browse/PLAT-11) | High | both | Dodać CI rejestru i macierz zgodności CLI–template |
| [PLAT-12](https://cichongabriel03.atlassian.net/browse/PLAT-12) | High | both | Chronić cały cykl zmian plików i stanu jedną blokadą |
| [PLAT-13](https://cichongabriel03.atlassian.net/browse/PLAT-13) | Highest | both | Wiązać apply i resume z niezmiennym zatwierdzonym planem |
| [PLAT-14](https://cichongabriel03.atlassian.net/browse/PLAT-14) | High | both | Renderować i uzgadniać pliki transakcyjnie |
| [PLAT-15](https://cichongabriel03.atlassian.net/browse/PLAT-15) | High | both | Zabezpieczyć granice workspace przed symlinkami |
| [PLAT-16](https://cichongabriel03.atlassian.net/browse/PLAT-16) | Medium | both | Domknąć walidację kontraktu manifestu i wartości |
| [PLAT-17](https://cichongabriel03.atlassian.net/browse/PLAT-17) | High | both | Wprowadzić jawne zaufanie i blokadę wersji szablonów |
| [PLAT-18](https://cichongabriel03.atlassian.net/browse/PLAT-18) | High | both | Zapewnić wersjonowany toolchain bez kolizji instalacji |
| [PLAT-19](https://cichongabriel03.atlassian.net/browse/PLAT-19) | High | both | Ujednolicić redakcję sekretów w logach i planach |
| [PLAT-20](https://cichongabriel03.atlassian.net/browse/PLAT-20) | High | both | Naprawić preflight, timeouty i kontrolę tożsamości celu |
| [PLAT-21](https://cichongabriel03.atlassian.net/browse/PLAT-21) | High | both | Domknąć cancel, timeout i resume po niepewnym wyniku |
| [PLAT-22](https://cichongabriel03.atlassian.net/browse/PLAT-22) | Medium | both | Dodać rzeczywiste health/status z odróżnieniem checkpointów |
| [PLAT-23](https://cichongabriel03.atlassian.net/browse/PLAT-23) | Medium | both | Ustabilizować kontrakt JSON, błędów i kodów wyjścia |
| [PLAT-24](https://cichongabriel03.atlassian.net/browse/PLAT-24) | Medium | both | Uczynić init atomowym i niezależnym od nazwy szablonu |
| [PLAT-25](https://cichongabriel03.atlassian.net/browse/PLAT-25) | High | portfolio | Doprowadzić lokalny stack do powtarzalnego ingress i pinów |
| [PLAT-26](https://cichongabriel03.atlassian.net/browse/PLAT-26) | High | both | Zapewnić idempotencję i własność zasobów lokalnego lifecycle |
| [PLAT-27](https://cichongabriel03.atlassian.net/browse/PLAT-27) | High | portfolio | Dostarczyć aplikację demo i pełną ścieżkę GitOps |
| [PLAT-28](https://cichongabriel03.atlassian.net/browse/PLAT-28) | Medium | portfolio | Dostarczyć dashboardy i testowalny alert dla demo |
| [PLAT-29](https://cichongabriel03.atlassian.net/browse/PLAT-29) | High | both | Zweryfikować izolację namespace, RBAC i obsługę sekretów |
| [PLAT-30](https://cichongabriel03.atlassian.net/browse/PLAT-30) | High | portfolio | Uruchamiać Kind E2E z awarią, resume i cleanup w CI |
| [PLAT-31](https://cichongabriel03.atlassian.net/browse/PLAT-31) | High | engineering | Zweryfikować Terraform EKS i wykonywanie zapisanego planu |
| [PLAT-32](https://cichongabriel03.atlassian.net/browse/PLAT-32) | High | engineering | Zabezpieczyć Terraform state i odtwarzanie workspace |
| [PLAT-33](https://cichongabriel03.atlassian.net/browse/PLAT-33) | High | engineering | Domknąć AWS IAM, storage, ingress i dostęp operatora |
| [PLAT-34](https://cichongabriel03.atlassian.net/browse/PLAT-34) | High | engineering | Zapewnić kompletne i wznawialne sprzątanie AWS |
| [PLAT-35](https://cichongabriel03.atlassian.net/browse/PLAT-35) | High | engineering | Przeprowadzić odbiór EKS z budżetem i dowodami kosztów |
| [PLAT-36](https://cichongabriel03.atlassian.net/browse/PLAT-36) | High | portfolio | Wydać instalowalne CLI z checksumami i pinem rejestru |
| [PLAT-37](https://cichongabriel03.atlassian.net/browse/PLAT-37) | High | portfolio | Przygotować pakiet portfolio i demo od czystej instalacji |
| [PLAT-38](https://cichongabriel03.atlassian.net/browse/PLAT-38) | High | engineering | Przeprowadzić porównawcze eksperymenty i pomiary |
| [PLAT-39](https://cichongabriel03.atlassian.net/browse/PLAT-39) | High | engineering | Opracować dokumentację pracy inżynierskiej z traceability |
| [PLAT-40](https://cichongabriel03.atlassian.net/browse/PLAT-40) | Medium | engineering | Zamknąć projekt wydaniem odtwarzalnym i pakietem obrony |
| [PLAT-41](https://cichongabriel03.atlassian.net/browse/PLAT-41) | Medium | both | Ustalić licencję, politykę zależności i utrzymania |

## Utrzymanie analizy

Przed rozpoczęciem taska sprawdź aktualny kod i Jira. Po zakończeniu zapisz dowody w zadaniu/PR; statyczny backlog.json nie jest źródłem bieżących statusów. Dla Blokada używaj etykiety blocked i komentarza z warunkiem odblokowania, dopóki przestrzeń nie ma osobnego statusu.
