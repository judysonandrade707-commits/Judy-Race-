package main

import (
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Group string

const (
	National      Group = "Nacional"
	International Group = "Internacional"
)

var Challenges = []string{"BEST LOOK", "ROAST", "TALENT SHOW", "MARKETING CHALLENGE"}

type Drag struct {
	ID          int                `json:"id"`
	Name        string             `json:"name"`
	Group       Group              `json:"group"`
	Photo       string             `json:"photo"`
	Scores      map[string]float64 `json:"scores"`
	Preferences []string           `json:"preferences,omitempty"`
	Rankings    map[string][]string `json:"rankings,omitempty"`
}

type Store struct {
	NextID int                `json:"next_id"`
	Drags  []Drag             `json:"drags"`
	Songs  map[Group][]string `json:"songs"`
}

func defaultSongs() map[Group][]string {
	return map[Group][]string{
		National:      {"Música Nacional 1", "Música Nacional 2", "Música Nacional 3", "Música Nacional 4", "Música Nacional 5", "Música Nacional 6"},
		International: {"Música Internacional 1", "Música Internacional 2", "Música Internacional 3", "Música Internacional 4", "Música Internacional 5", "Música Internacional 6"},
	}
}

type App struct {
	mu    sync.RWMutex
	store Store
	tmpl  *template.Template
}

type Battle struct{ A, B Drag; Song string; RankA, RankB int }
type RoundView struct { Number int; Saved bool; Battles []Battle }
type GroupView struct {
	Name       Group
	Songs      []string
	Drags      []Drag
	Top4       []Drag
	Finalists  []Drag
	Battles    []Battle
	Rounds     []RoundView
	Eliminated []Drag
}
type PageData struct{ Challenges []string; Groups []GroupView; Message string }
type RankingPageData struct { Group Group; Round int; Songs []string; Drag Drag; Message string }

func total(d Drag) float64 { var s float64; for _, c := range Challenges { s += d.Scores[c] }; return s }
func roundKey(round int) string { return fmt.Sprintf("rodada-%d", round) }
func prefsFor(d Drag, round int) []string { if d.Rankings != nil { if p := d.Rankings[roundKey(round)]; len(p) == 6 { return p } }; return nil }
func scoreForRound(d Drag, song string, round int) int { for i, s := range prefsFor(d, round) { if s == song { return 6-i } }; return 0 }
func rankForRound(d Drag, song string, round int) int { for i, s := range prefsFor(d, round) { if s == song { return i+1 } }; return 99 }
func rankFor(d Drag, song string) int { return rankForRound(d, song, 1) }

func main() {
	_ = os.MkdirAll("data/uploads", 0755)
	funcs := template.FuncMap{"total": total, "score": func(d Drag, c string) float64 { return d.Scores[c] }, "rank": rankFor, "rankRound": rankForRound, "inc": func(i int) int { return i + 1 }, "seq6": func() []int { return []int{1, 2, 3, 4, 5, 6} }}
	t := template.Must(template.New("index.html").Funcs(funcs).ParseFiles("templates/index.html", "templates/ranking.html"))
	a := &App{tmpl: t}; a.load()
	http.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))
	http.Handle("/uploads/", http.StripPrefix("/uploads/", http.FileServer(http.Dir("data/uploads"))))
	http.HandleFunc("/", a.index)
	http.HandleFunc("/drag/add", a.addDrag)
	http.HandleFunc("/drag/update", a.updateDrag)
	http.HandleFunc("/drag/delete", a.deleteDrag)
	http.HandleFunc("/score/update", a.updateScore)
	http.HandleFunc("/songs/update", a.updateSongs)
	http.HandleFunc("/ranking", a.rankingPage)
	http.HandleFunc("/preferences/update", a.updatePreferences)
	log.Println("Judy Race disponível em http://localhost:8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}

func (a *App) load() {
	b, err := os.ReadFile("data/store.json")
	if err == nil {
		// Migração segura: se o arquivo antigo tiver songs como lista, preserva as drags e recria músicas por grupo.
		var raw struct { NextID int `json:"next_id"`; Drags []Drag `json:"drags"`; Songs json.RawMessage `json:"songs"` }
		if json.Unmarshal(b, &raw) == nil { a.store.NextID, a.store.Drags = raw.NextID, raw.Drags; _ = json.Unmarshal(raw.Songs, &a.store.Songs) }
	}
	if a.store.NextID == 0 { a.store.NextID = 1 }
	if a.store.Songs == nil { a.store.Songs = defaultSongs() }
	defaults := defaultSongs()
	for _, g := range []Group{National, International} { if len(a.store.Songs[g]) != 6 { a.store.Songs[g] = append([]string(nil), defaults[g]...) } }
	for i := range a.store.Drags { if a.store.Drags[i].Scores == nil { a.store.Drags[i].Scores = map[string]float64{} }; if a.store.Drags[i].Rankings == nil { a.store.Drags[i].Rankings = map[string][]string{} }; a.normalizePreferences(&a.store.Drags[i]) }
	a.save()
}
func (a *App) save() { b, _ := json.MarshalIndent(a.store, "", "  "); _ = os.WriteFile("data/store.json", b, 0644) }
func (a *App) normalizePreferences(d *Drag) { songs := a.store.Songs[d.Group]; seen := map[string]bool{}; out := []string{}; for _, p := range d.Preferences { for _, s := range songs { if p == s && !seen[s] { out = append(out, s); seen[s] = true } } }; for _, s := range songs { if !seen[s] { out = append(out, s) } }; d.Preferences = out }
func redirect(w http.ResponseWriter, r *http.Request, msg string) { http.Redirect(w, r, "/?msg="+strings.ReplaceAll(msg, " ", "+"), http.StatusSeeOther) }

func (a *App) index(w http.ResponseWriter, r *http.Request) {
	a.mu.RLock(); defer a.mu.RUnlock()
	data := PageData{Challenges: Challenges, Message: r.URL.Query().Get("msg")}
	for _, g := range []Group{National, International} {
		var ds []Drag; for _, d := range a.store.Drags { if d.Group == g { ds = append(ds, d) } }
		sort.SliceStable(ds, func(i, j int) bool { if total(ds[i]) == total(ds[j]) { return ds[i].Name < ds[j].Name }; return total(ds[i]) > total(ds[j]) })
		topN := 4; if len(ds) < topN { topN = len(ds) }; top4 := append([]Drag(nil), ds[:topN]...); elim := append([]Drag(nil), ds[topN:]...)
		finN := 1; if len(top4) < finN { finN = len(top4) }
		gv := GroupView{Name: g, Songs: append([]string(nil), a.store.Songs[g]...), Drags: ds, Top4: top4, Finalists: append([]Drag(nil), top4[:finN]...), Eliminated: elim}
		if len(top4) == 4 {
			for round := 1; round <= 3; round++ {
				saved := true
				for _, d := range top4 { if len(prefsFor(d, round)) != 6 { saved = false } }
				rv := RoundView{Number: round, Saved: saved}
				if saved { rv.Battles = makeRoundBattles(top4, gv.Songs, round) }
				gv.Rounds = append(gv.Rounds, rv)
			}
		}
		data.Groups = append(data.Groups, gv)
	}
	_ = a.tmpl.ExecuteTemplate(w, "index.html", data)
}

func savePhoto(f multipart.File, h *multipart.FileHeader) (string, error) { ext := strings.ToLower(filepath.Ext(h.Filename)); if ext != ".jpg" && ext != ".jpeg" && ext != ".png" && ext != ".webp" { return "", fmt.Errorf("formato inválido") }; name := fmt.Sprintf("%d%s", time.Now().UnixNano(), ext); out, err := os.Create(filepath.Join("data/uploads", name)); if err != nil { return "", err }; defer out.Close(); _, err = io.Copy(out, f); return "/uploads/" + name, err }

func (a *App) addDrag(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost { http.NotFound(w, r); return }; _ = r.ParseMultipartForm(10 << 20)
	name, group := strings.TrimSpace(r.FormValue("name")), Group(r.FormValue("group")); if name == "" || (group != National && group != International) { redirect(w, r, "Dados inválidos"); return }
	photo := ""; if f, h, err := r.FormFile("photo"); err == nil { defer f.Close(); photo, _ = savePhoto(f, h) }
	a.mu.Lock(); defer a.mu.Unlock(); d := Drag{ID: a.store.NextID, Name: name, Group: group, Photo: photo, Scores: map[string]float64{}, Rankings: map[string][]string{}}; d.Preferences = append([]string(nil), a.store.Songs[group]...); a.store.Drags = append(a.store.Drags, d); a.store.NextID++; a.save(); redirect(w, r, "Drag adicionada")
}
func (a *App) updateDrag(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost { http.NotFound(w, r); return }; _ = r.ParseMultipartForm(10 << 20); id, _ := strconv.Atoi(r.FormValue("id"))
	a.mu.Lock(); defer a.mu.Unlock(); for i := range a.store.Drags { d := &a.store.Drags[i]; if d.ID != id { continue }; oldGroup := d.Group; d.Name = strings.TrimSpace(r.FormValue("name")); d.Group = Group(r.FormValue("group")); if d.Scores == nil { d.Scores = map[string]float64{} }; for _, c := range Challenges { v, _ := strconv.ParseFloat(strings.ReplaceAll(r.FormValue("score_"+c), ",", "."), 64); d.Scores[c] = v }; if oldGroup != d.Group { d.Preferences = append([]string(nil), a.store.Songs[d.Group]...) }; if f, h, err := r.FormFile("photo"); err == nil { defer f.Close(); if p, e := savePhoto(f, h); e == nil { d.Photo = p } }; break }; a.save(); redirect(w, r, "Dados e pontuações atualizados")
}
func (a *App) updateScore(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost { http.NotFound(w, r); return }
	id, _ := strconv.Atoi(r.FormValue("id"))
	challenge := r.FormValue("challenge")
	valid := false
	for _, c := range Challenges { if c == challenge { valid = true; break } }
	if !valid { redirect(w, r, "Desafio inválido"); return }
	value, _ := strconv.ParseFloat(strings.ReplaceAll(r.FormValue("score"), ",", "."), 64)
	a.mu.Lock()
	defer a.mu.Unlock()
	for i := range a.store.Drags {
		if a.store.Drags[i].ID == id {
			if a.store.Drags[i].Scores == nil { a.store.Drags[i].Scores = map[string]float64{} }
			a.store.Drags[i].Scores[challenge] = value
			break
		}
	}
	a.save()
	redirect(w, r, "Pontuação atualizada")
}

func (a *App) deleteDrag(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost { http.NotFound(w, r); return }; id, _ := strconv.Atoi(r.FormValue("id")); a.mu.Lock(); defer a.mu.Unlock(); for i, d := range a.store.Drags { if d.ID == id { if strings.HasPrefix(d.Photo, "/uploads/") { _ = os.Remove(filepath.Join("data/uploads", filepath.Base(d.Photo))) }; a.store.Drags = append(a.store.Drags[:i], a.store.Drags[i+1:]...); break } }; a.save(); redirect(w, r, "Drag removida")
}
func (a *App) updateSongs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost { http.NotFound(w, r); return }; group := Group(r.FormValue("group")); if group != National && group != International { redirect(w, r, "Grupo inválido"); return }
	a.mu.Lock(); defer a.mu.Unlock(); old := append([]string(nil), a.store.Songs[group]...); ns := make([]string, 6); for i := 0; i < 6; i++ { ns[i] = strings.TrimSpace(r.FormValue(fmt.Sprintf("song%d", i))); if ns[i] == "" { ns[i] = fmt.Sprintf("Música %s %d", group, i+1) } }; a.store.Songs[group] = ns
	for di := range a.store.Drags { d := &a.store.Drags[di]; if d.Group != group { continue }; for pi, p := range d.Preferences { for i, o := range old { if p == o { d.Preferences[pi] = ns[i] } } }; a.normalizePreferences(d) }; a.save(); redirect(w, r, "Músicas do grupo "+string(group)+" atualizadas")
}
func (a *App) rankingPage(w http.ResponseWriter, r *http.Request) {
	group := Group(r.URL.Query().Get("group")); round, _ := strconv.Atoi(r.URL.Query().Get("round")); dragID, _ := strconv.Atoi(r.URL.Query().Get("drag"))
	if (group != National && group != International) || round < 1 || round > 3 || dragID < 1 { http.Error(w, "Grupo, rodada ou participante inválido", http.StatusBadRequest); return }
	a.mu.RLock(); defer a.mu.RUnlock()
	var selected *Drag
	for i := range a.store.Drags { if a.store.Drags[i].ID == dragID && a.store.Drags[i].Group == group { copy := a.store.Drags[i]; selected = &copy; break } }
	if selected == nil { http.Error(w, "Participante não encontrada", http.StatusNotFound); return }
	data := RankingPageData{Group: group, Round: round, Songs: a.store.Songs[group], Drag: *selected, Message: r.URL.Query().Get("msg")}
	_ = a.tmpl.ExecuteTemplate(w, "ranking.html", data)
}
func (a *App) updatePreferences(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost { http.NotFound(w,r); return }
	group := Group(r.FormValue("group")); round, _ := strconv.Atoi(r.FormValue("round")); dragID, _ := strconv.Atoi(r.FormValue("drag"))
	if (group != National && group != International) || round < 1 || round > 3 || dragID < 1 { http.Error(w,"Dados inválidos",400); return }
	a.mu.Lock(); defer a.mu.Unlock()
	found := false
	for i := range a.store.Drags {
		d := &a.store.Drags[i]; if d.ID != dragID || d.Group != group { continue }
		prefs, seen := []string{}, map[string]bool{}
		for n:=1; n<=6; n++ { song:=r.FormValue(fmt.Sprintf("pref%d",n)); if song!="" && !seen[song] { prefs=append(prefs,song); seen[song]=true } }
		if len(prefs) != 6 { http.Error(w,"Cada música deve aparecer uma única vez no ranking",400); return }
		if d.Rankings == nil { d.Rankings=map[string][]string{} }; d.Rankings[roundKey(round)] = append([]string(nil), prefs...); found=true; break
	}
	if !found { http.Error(w,"Participante não encontrada",404); return }
	a.save(); http.Redirect(w,r,fmt.Sprintf("/?msg=Ranking+individual+salvo#lalaparuza"),http.StatusSeeOther)
}

func makeRoundBattles(ds []Drag, songs []string, round int) []Battle {
	pairRounds := [][][2]int{{{0,1},{2,3}},{{0,2},{1,3}},{{0,3},{1,2}}}; pairs:=pairRounds[round-1]
	bestA,bestB,bestScore:=0,1,-1
	for a:=0;a<len(songs);a++ { for b:=0;b<len(songs);b++ { if a==b {continue}; p1,p2:=pairs[0],pairs[1]; sc:=scoreForRound(ds[p1[0]],songs[a],round)+scoreForRound(ds[p1[1]],songs[a],round)+scoreForRound(ds[p2[0]],songs[b],round)+scoreForRound(ds[p2[1]],songs[b],round); if sc>bestScore {bestScore,bestA,bestB=sc,a,b} } }
	indexes:=[]int{bestA,bestB}; out:=[]Battle{}
	for i,p:=range pairs { song:=songs[indexes[i]]; out=append(out,Battle{A:ds[p[0]],B:ds[p[1]],Song:song,RankA:rankForRound(ds[p[0]],song,round),RankB:rankForRound(ds[p[1]],song,round)}) }
	return out
}

