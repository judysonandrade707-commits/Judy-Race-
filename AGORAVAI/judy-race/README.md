# Judy Race em Go

Versão com ranking único por participante, seguindo a lógica do Rate a Song fornecido:
- cada participante classifica as 6 músicas uma única vez;
- os 4 rankings secretos geram as 6 batalhas de uma vez;
- 3 rodadas, 2 batalhas por rodada;
- cada participante enfrenta as outras 3 uma vez;
- cada música é usada uma vez;
- menor soma de posições, depois mais primeiras escolhas;
- compensação de preferência nas rodadas seguintes;
- pontos acumulados e ordem de cadastro como desempates;
- brackets Nacional e Internacional independentes;
- pontuações de cada desafio salvas em lote por grupo.

Execute: `go run .`

## Cadastro em lote
O cadastro inicial possui 5 posições para Nacional e 5 para Internacional e um único botão **Adicionar todas as drags**. Posições sem nome são ignoradas.
