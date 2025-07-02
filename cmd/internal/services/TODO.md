# Технический долг: Рефакторинг `processProposal`

**Дата:** 02.07.2025

## Проблема

В текущей реализации метод `processProposal` использует флаг `isBaseline`, чтобы менять свое поведение в зависимости от того, обрабатывается ли базовое предложение или предложение от подрядчика. Это нарушает Принцип единственной ответственности (Single Responsibility Principle) и усложняет код, так как логика ветвится внутри одной функции.

Кроме того, метод `ProcessProposalAdditionalInfo` также содержит проверку этого флага, что "размазывает" логику по нескольким методам.

## Решение

Необходимо провести рефакторинг, чтобы сделать архитектуру более явной и предсказуемой:

1.  **Разделить `processProposal`:** Вместо одной универсальной функции с флагом, создать две отдельные приватные функции:
    * `processBaselineProposal(ctx, qtx, lotID, proposalAPI)`
    * `processContractorProposal(ctx, qtx, lotID, proposalAPI)`

2.  **Перенести логику:**
    * В `processBaselineProposal` захардкодить получение "Инициатора" и вызвать низкоуровневый хелпер `createProposalWithItems`, передав `isBaseline: true`.
    * В `processContractorProposal` вызывать `GetOrCreateContractor` для реального подрядчика и также вызывать `createProposalWithItems`, передав `isBaseline: false`.

3.  **Очистить `ProcessProposalAdditionalInfo`:** Убрать из этого метода проверку `if isBaseline`. Логику "не вызывать для базового предложения" перенести в `createProposalWithItems`, где это будет более явно.

4.  **Обновить `processLot`:** Главный метод `processLot` должен будет вызывать не одну, а две новые функции: `processBaselineProposal` и (в цикле) `processContractorProposal`.

## Результат

После рефакторинга каждая функция будет иметь одну четкую обязанность, код станет более читаемым, и исчезнет необходимость мысленно отслеживать, как флаг `isBaseline` влияет на поведение системы.