# Установка обновления и публикация

Распакуй архив обновления в корень репозитория с заменой файлов. Запусти
команды из раздела «Обновление работающего Docker-запуска» в [TEAM-START.md](TEAM-START.md).

После пересборки открой сайт и задай вопрос. У коллеги с пустой базой первый запуск
требует `-ImportCorpus` и двух JSON-файлов из инструкции.

## GitHub после проверки запуска

Из корня репозитория:

```powershell
git status --short
git branch --show-current
git check-ignore .env.team
git ls-files .env .env.team
```

Ожидаемая ветка Frontend. Последняя команда НЕ должна показывать файлы.
Если показывает — перед публикацией убрать секретные файлы из индекса через git rm --cached и проверить, не были ли ключи опубликованы раньше.

```powershell
git add -- .gitignore .env.example README.md TEAM-START.md RELEASE-CHECKS.md INSTALL-UPDATE.md compose.team.yml scripts backend frontend
git diff --cached --stat
git diff --cached --check
```

Проверить список: никаких секретов, корпусов, node_modules, dist или архивов.
Если всё в порядке:

```powershell
git commit -m "Ship Buddy chat with Docker setup and automatic demo access"
git push -u origin Frontend
```

Если push отклонён из-за коммитов коллег, выполнить git fetch origin и разобраться с расхождениями через обычное слияние. Не использовать force push.
После публикации создать PR Frontend -> main с описанием изменений и проверок.
Передача архива в ChatGPT не является git push: команды выполняются в локальном клоне с доступом к GitHub.
