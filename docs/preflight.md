# Preflight i wybór celu

`platformctl preflight --strict --phase bootstrap` sprawdza narzędzia,
credentials z manifestu, Docker (jeśli wymagany), możliwość zapisu workspace
i poprawność lokalnego stanu. Bootstrap nie wymaga istniejącego kontekstu
Kubernetes. Dla istniejącego klastra użyj `--phase apply` lub `--phase destroy`.

Każdy proces diagnostyczny ma domyślnie 5 s oraz wspólny limit 32 KiB stdout
i stderr. `--check-timeout 100ms..30s` zmienia limit pojedynczego procesu
preflight; cały przebieg ma limit 2 min. Timeout, nieudane uruchomienie,
przekroczony output i nieudana kontrola wersji są błędami kontroli wymaganej.
`--strict` zwraca kod 1. Bez strict wynik JSON ma `ok: false`, a exit pozostaje
0. Diagnostyka nie publikuje treści odpowiedzi poleceń credential/identity.
Na macOS/Linux zakończenie kontroli sprząta jej grupę procesów; to nie jest
sandbox dla procesu, który sam odłącza się od grupy.

Preflight nie instaluje narzędzi ani nie tworzy katalogu stanu. Krótkie pliki
próbne zapisu są usuwane. Apply/destroy zachowują dotychczasowe przygotowanie
zarządzanych narzędzi, następnie wykonują strict preflight przed generowaniem
plików i uruchomieniem workflow. Brakujące narzędzia dla standalone preflight
należy przygotować osobno. Wbudowane checks nie wywołują cloud apply/destroy ani
`kubectl config use-context`/`aws eks update-kubeconfig`. Credential command
z manifestu pozostaje zaufanym programem autora; CLI nie udowadnia, że
dowolny skrypt nie wykonuje mutacji. Używaj tu wyłącznie read-only poleceń.

## Opcjonalna deklaracja oczekiwanej tożsamości

Stare platform.yaml pozostają poprawne. Bez `target` wynik zawiera ostrzeżenie,
że tożsamość celu nie została zweryfikowana. Nie traktuj sukcesu legacy
preflight jako potwierdzenia konta lub klastra. Target jest częścią istniejącego
digestu planu; zmiana deklaracji wymaga nowego podglądu i zatwierdzenia.

```yaml
target:
  cluster_name: demo-platform # musi odpowiadać values.cluster_name
  aws:
    account: "123456789012"
    region: eu-central-1
    profile: develop
  kubernetes:
    context: arn:aws:eks:eu-central-1:123456789012:cluster/demo-platform
    cluster: arn:aws:eks:eu-central-1:123456789012:cluster/demo-platform
    server: https://example.eks.invalid
```

Każda sekcja jest opcjonalna; puste `target: {}` jest błędem. AWS wymaga
wszystkich trzech pól. Konto jest porównywane z read-only STS GetCallerIdentity
wykonanym z jawnym profilem i regionem. CLI przekazuje ten sam profile i region do środowiska procesów workflow;
AWS_PROFILE/AWS_DEFAULT_PROFILE i AWS_REGION/AWS_DEFAULT_REGION są lokalnie
ustawiane z target. Odziedziczone static/web-identity credential variables są
puste dla tych procesów, żeby nie wybrały innego konta niż zweryfikowany profil.
Jeśli manifest definiuje wartości region/aws_profile, muszą być takie same;
kroki Terraform z AWS target wymagają obu tych deklarowanych wartości.
Jawne sprzeczne profile/region/context/cluster/server w argumentach narzędzi
są odrzucane przed generowaniem plików. Alternatywne endpointy AWS z
AWS_ENDPOINT_URL* lub argumentu --endpoint-url są odrzucane. Dla Terraform
AWS target nie dopuszcza TF_CLI_ARGS*, -chdir, -var ani -var-file, które mogłyby
podmienić zweryfikowane selektory poza deklaracją.
Nie wpisuj credentiali do target. Profil musi działać bez odziedziczonych
static/web-identity credential variables.

Kubernetes wymaga context, nazwy cluster w kubeconfig i dokładnego HTTPS server.
Sprawdzane są razem przez lokalne `kubectl config view --minify --output json`,
bez raw/flatten i bez modyfikacji kubeconfig. To potwierdzenie wybranej
konfiguracji/endpointu, nie readiness, UID zdalnego klastra ani udany deploy.
Nazwy i PATH/config narzędzi pozostają zaufaną konfiguracją operatora.

## Utworzenie nowego klastra

`target.bootstrap: true` w platform.yaml jawnie oznacza tworzenie klastra
przez `platformctl apply --yes`. Ta polityka jest częścią zatwierdzanego digestu,
nie flagą zmieniającą zachowanie po zatwierdzeniu planu.
Przed generowaniem sprawdzane są dostępne assertions AWS/region/cluster_name;
kontekst Kubernetes może jeszcze nie istnieć. Przed każdym krokiem kubectl,
helm (poza repo/version) oraz krokiem oznaczonym `requires_cluster: true`
sprawdzana jest już pełna zadeklarowana tożsamość Kubernetes. Kontrola jest
ponawiana także przed retry. Apply z nowym target bez bootstrap oraz destroy z deklaracją Kubernetes
wymagają jej od początku. Legacy apply bez target zachowuje możliwość
utworzenia klastra bez gotowego kontekstu, z ostrzeżeniem o braku identity check.
Readiness destroy odpytuje kubectl tylko przy rzeczywistych krokach kierowanych
do klastra; samo kind delete lub terraform destroy nie wymaga kontekstu.

```yaml
steps:
  apply:
    - name: custom cluster operation
      command: sh
      args: [./deploy.sh]
      requires_cluster: true
```

Autor zaufanego manifestu musi honorować zadeklarowany profil/region także
w konfiguracji Terraform/provider (bez override konta lub assume_role na inne
konto) i we własnych skryptach. CLI nie dowodzi semantyki dowolnego HCL/skryptu.
Autor musi oznaczyć własne skrypty/niestandardowe programy
wykonujące operacje na klastrze. CLI nie analizuje programu shell ani nie
przepisuje jego argumentów. Ominięcie markerów, zmiana kontekstu wewnątrz
skryptu lub zewnętrzna zmiana celu między kontrolą a użyciem nie są izolowane.
Nie używaj bootstrap do obsługi istniejącego klastra. Po utworzeniu klastra
usuń bootstrap albo ustaw false, a następnie ponów plan i zatwierdzenie.

Zmiana formatu state.json nie jest potrzebna. Przy wycofaniu CLI usuń nową
opcjonalną sekcję target i requires_cluster ze swoich konfiguracji/manifestów
dopiero po świadomej ocenie utraty tych kontroli; stary strict parser odrzuci
nieznane pola zamiast je ignorować.
