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
const ( National Group = "Nacional"; International Group = "Internacional" )
var challenges = []string{"BEST LOOK", "ROAST", "TALENT SHOW", "MARKETING CHALLENGE"}

type Drag struct { ID int `json:"id"`; Name string `json:"name"`; Group Group `json:"group"`; Photo string `json:"photo"`; Scores map[string]float64 `json:"scores"`; Ranking []string `json:"ranking,omitempty"` }
type Store struct { NextID int `json:"next_id"`; Drags []Drag `json:"drags"`; Songs map[Group][]string `json:"songs"` }
type Battle struct { A, B Drag; Song string; RankA, RankB int }
type Round struct { Number int; Battles []Battle }
type GroupView struct { Name Group; Songs []string; Drags, Top4, Finalists, Eliminated []Drag; RankingComplete bool; Rounds []Round }
type PageData struct { Challenges []string; Groups []GroupView; Message string }
type RankingData struct { Group Group; Songs []string; Drag Drag }
type App struct { mu sync.RWMutex; store Store; tmpl *template.Template }

func defaultSongs() map[Group][]string { return map[Group][]string{ National: {"Música Nacional 1","Música Nacional 2","Música Nacional 3","Música Nacional 4","Música Nacional 5","Música Nacional 6"}, International: {"Música Internacional 1","Música Internacional 2","Música Internacional 3","Música Internacional 4","Música Internacional 5","Música Internacional 6"}, } }
func total(d Drag) float64 { var v float64; for _, c := range challenges { v += d.Scores[c] }; return v }
func rank(d Drag, song string) int { for i, s := range d.Ranking { if s == song { return i+1 } }; return 99 }

func main() {
    _ = os.MkdirAll("data/uploads", 0755)
    funcs := template.FuncMap{"total":total,"score":func(d Drag,c string)float64{return d.Scores[c]},"rank":rank,"inc":func(i int)int{return i+1},"seq6":func()[]int{return []int{1,2,3,4,5,6}}}
    t := template.Must(template.New("index.html").Funcs(funcs).ParseFiles("templates/index.html","templates/ranking.html"))
    app := &App{tmpl:t}; app.load()
    http.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))
    http.Handle("/uploads/", http.StripPrefix("/uploads/", http.FileServer(http.Dir("data/uploads"))))
    http.HandleFunc("/", app.home); http.HandleFunc("/drags/add-batch", app.addDragsBatch); http.HandleFunc("/drag/add", app.addDrag); http.HandleFunc("/drag/update", app.updateDrag); http.HandleFunc("/drag/delete", app.deleteDrag); http.HandleFunc("/songs/update", app.updateSongs); http.HandleFunc("/scores/update", app.updateScores); http.HandleFunc("/ranking", app.rankingPage); http.HandleFunc("/ranking/save", app.saveRanking)
    log.Println("Judy Race disponível em http://localhost:8080"); log.Fatal(http.ListenAndServe(":8080", nil))
}
func (a *App) load(){ b,e:=os.ReadFile("data/store.json"); if e==nil { _=json.Unmarshal(b,&a.store) }; if a.store.NextID==0 {a.store.NextID=1}; if a.store.Songs==nil {a.store.Songs=defaultSongs()}; defaults:=defaultSongs(); for _,g:=range []Group{National,International}{if len(a.store.Songs[g])!=6{a.store.Songs[g]=defaults[g]}}; for i:=range a.store.Drags {if a.store.Drags[i].Scores==nil{a.store.Drags[i].Scores=map[string]float64{}}}; a.save() }
func (a *App) save(){b,_:=json.MarshalIndent(a.store,"","  "); _=os.WriteFile("data/store.json",b,0644)}
func redirect(w http.ResponseWriter,r *http.Request,msg,anchor string){http.Redirect(w,r,"/?msg="+strings.ReplaceAll(msg," ","+")+anchor,http.StatusSeeOther)}
func (a *App) home(w http.ResponseWriter,r *http.Request){a.mu.RLock(); defer a.mu.RUnlock(); data:=PageData{Challenges:challenges,Message:r.URL.Query().Get("msg")}; for _,g:=range []Group{National,International}{ds:=[]Drag{};for _,d:=range a.store.Drags{if d.Group==g{ds=append(ds,d)}};sort.SliceStable(ds,func(i,j int)bool{if total(ds[i])==total(ds[j]){return ds[i].ID<ds[j].ID};return total(ds[i])>total(ds[j])});n:=4;if len(ds)<n{n=len(ds)};top:=append([]Drag(nil),ds[:n]...);gv:=GroupView{Name:g,Songs:append([]string(nil),a.store.Songs[g]...),Drags:ds,Top4:top,Eliminated:append([]Drag(nil),ds[n:]...)};if len(ds)>0{gv.Finalists=[]Drag{ds[0]}};if len(top)==4{complete:=true;for _,d:=range top{if len(d.Ranking)!=6{complete=false}};gv.RankingComplete=complete;if complete{gv.Rounds=generateAllRounds(top,gv.Songs)}};data.Groups=append(data.Groups,gv)};if err:=a.tmpl.ExecuteTemplate(w,"index.html",data);err!=nil{log.Println("template:",err)}}
func savePhoto(f multipart.File,h *multipart.FileHeader)(string,error){ext:=strings.ToLower(filepath.Ext(h.Filename));if ext!=".jpg"&&ext!=".jpeg"&&ext!=".png"&&ext!=".webp"{return "",fmt.Errorf("formato inválido")};name:=fmt.Sprintf("%d%s",time.Now().UnixNano(),ext);out,err:=os.Create(filepath.Join("data/uploads",name));if err!=nil{return "",err};defer out.Close();_,err=io.Copy(out,f);return "/uploads/"+name,err}
func (a *App) addDragsBatch(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost { http.NotFound(w,r); return }
	if err := r.ParseMultipartForm(40 << 20); err != nil { http.Error(w,"Erro ao ler cadastro",400); return }
	a.mu.Lock(); defer a.mu.Unlock()
	added := 0
	for _, item := range []struct{ Prefix string; Group Group }{{"national",National},{"international",International}} {
		for slot:=1; slot<=5; slot++ {
			nameKey:=fmt.Sprintf("%s_name_%d",item.Prefix,slot); photoKey:=fmt.Sprintf("%s_photo_%d",item.Prefix,slot)
			name:=strings.TrimSpace(r.FormValue(nameKey)); if name=="" { continue }
			ph:=""; if f,h,e:=r.FormFile(photoKey); e==nil { defer f.Close(); ph,_=savePhoto(f,h) }
			a.store.Drags=append(a.store.Drags,Drag{ID:a.store.NextID,Name:name,Group:item.Group,Photo:ph,Scores:map[string]float64{}}); a.store.NextID++; added++
		}
	}
	a.save()
	if added==0 { redirect(w,r,"Nenhuma drag preenchida","#cadastro"); return }
	redirect(w,r,fmt.Sprintf("%d drags adicionadas",added),"#cadastro")
}

func (a *App) addDrag(w http.ResponseWriter,r *http.Request){_ = r.ParseMultipartForm(10<<20);g:=Group(r.FormValue("group"));name:=strings.TrimSpace(r.FormValue("name"));if name==""||(g!=National&&g!=International){redirect(w,r,"Dados inválidos","");return};photo:="";if f,h,e:=r.FormFile("photo");e==nil{defer f.Close();photo,_=savePhoto(f,h)};a.mu.Lock();defer a.mu.Unlock();a.store.Drags=append(a.store.Drags,Drag{ID:a.store.NextID,Name:name,Group:g,Photo:photo,Scores:map[string]float64{}});a.store.NextID++;a.save();redirect(w,r,"Drag adicionada","#cadastro")}
func (a *App) updateDrag(w http.ResponseWriter,r *http.Request){_ = r.ParseMultipartForm(10<<20);id,_:=strconv.Atoi(r.FormValue("id"));a.mu.Lock();defer a.mu.Unlock();for i:=range a.store.Drags{d:=&a.store.Drags[i];if d.ID!=id{continue};d.Name=strings.TrimSpace(r.FormValue("name"));if f,h,e:=r.FormFile("photo");e==nil{defer f.Close();if p,e:=savePhoto(f,h);e==nil{d.Photo=p}}};a.save();redirect(w,r,"Cadastro atualizado","#cadastro")}
func (a *App) deleteDrag(w http.ResponseWriter,r *http.Request){id,_:=strconv.Atoi(r.FormValue("id"));a.mu.Lock();defer a.mu.Unlock();for i,d:=range a.store.Drags{if d.ID==id{a.store.Drags=append(a.store.Drags[:i],a.store.Drags[i+1:]...);break}};a.save();redirect(w,r,"Drag excluída","#cadastro")}
func (a *App) updateSongs(w http.ResponseWriter,r *http.Request){g:=Group(r.FormValue("group"));a.mu.Lock();defer a.mu.Unlock();songs:=make([]string,6);for i:=0;i<6;i++{songs[i]=strings.TrimSpace(r.FormValue(fmt.Sprintf("song%d",i)));if songs[i]==""{songs[i]=fmt.Sprintf("Música %s %d",g,i+1)}};a.store.Songs[g]=songs;for i:=range a.store.Drags{if a.store.Drags[i].Group==g{a.store.Drags[i].Ranking=nil}};a.save();redirect(w,r,"Músicas atualizadas; rankings apagados","#musicas")}
func (a *App) updateScores(w http.ResponseWriter,r *http.Request){g:=Group(r.FormValue("group"));challenge:=r.FormValue("challenge");valid:=false;for _,c:=range challenges{if c==challenge{valid=true}};if !valid{redirect(w,r,"Desafio inválido","#desafios");return};a.mu.Lock();defer a.mu.Unlock();for i:=range a.store.Drags{d:=&a.store.Drags[i];if d.Group!=g{continue};v,_:=strconv.ParseFloat(strings.ReplaceAll(r.FormValue(fmt.Sprintf("score_%d",d.ID)),",","."),64);d.Scores[challenge]=v};a.save();redirect(w,r,"Todas as pontuações salvas","#desafios")}
func (a *App) rankingPage(w http.ResponseWriter,r *http.Request){g:=Group(r.URL.Query().Get("group"));id,_:=strconv.Atoi(r.URL.Query().Get("drag"));a.mu.RLock();defer a.mu.RUnlock();for _,d:=range a.store.Drags{if d.ID==id&&d.Group==g{_ = a.tmpl.ExecuteTemplate(w,"ranking.html",RankingData{Group:g,Songs:a.store.Songs[g],Drag:d});return}};http.NotFound(w,r)}
func (a *App) saveRanking(w http.ResponseWriter,r *http.Request){g:=Group(r.FormValue("group"));id,_:=strconv.Atoi(r.FormValue("drag"));order:=strings.Split(r.FormValue("order"),"\x1f");if len(order)!=6{http.Error(w,"Ranking inválido",400);return};seen:=map[string]bool{};for _,s:=range order{seen[s]=true};if len(seen)!=6{http.Error(w,"Não repita músicas",400);return};a.mu.Lock();defer a.mu.Unlock();for i:=range a.store.Drags{if a.store.Drags[i].ID==id&&a.store.Drags[i].Group==g{a.store.Drags[i].Ranking=append([]string(nil),order...);break}};a.save();redirect(w,r,"Ranking individual salvo","#lalaparuza")}

var matchings = [][2][2]int{{{0,1},{2,3}},{{0,2},{1,3}},{{0,3},{1,2}}}
type candidate struct{matching int;songs [2]int;score []float64}
func lessScore(a,b []float64)bool{for i:=range a{if a[i]!=b[i]{return a[i]<b[i]}};return false}
func generateAllRounds(ds []Drag,songs []string)[]Round{usedMatching:=map[int]bool{};usedSong:=map[int]bool{};debt:=make([]float64,4);rounds:=[]Round{};for roundNo:=1;roundNo<=3;roundNo++{var best *candidate;for mi,pairs:=range matchings{if usedMatching[mi]{continue};for sa:=0;sa<6;sa++{if usedSong[sa]{continue};for sb:=0;sb<6;sb++{if sa==sb||usedSong[sb]{continue};p0,p1:=pairs[0],pairs[1];positions:=[]int{rank(ds[p0[0]],songs[sa]),rank(ds[p0[1]],songs[sa]),rank(ds[p1[0]],songs[sb]),rank(ds[p1[1]],songs[sb])};sum,firsts:=0,0;for _,v:=range positions{sum+=v;if v==1{firsts++}};priority:=[]int{0,1,2,3};sort.SliceStable(priority,func(i,j int)bool{if debt[priority[i]]!=debt[priority[j]]{return debt[priority[i]]>debt[priority[j]]};if total(ds[priority[i]])!=total(ds[priority[j]]){return total(ds[priority[i]])>total(ds[priority[j]])};return priority[i]<priority[j]});score:=[]float64{float64(sum),float64(-firsts)};for _,pi:=range priority{song:=songs[sb];if pi==p0[0]||pi==p0[1]{song=songs[sa]};score=append(score,float64(rank(ds[pi],song)))};c:=&candidate{matching:mi,songs:[2]int{sa,sb},score:score};if best==nil||lessScore(c.score,best.score){best=c}}}};if best==nil{break};pairs:=matchings[best.matching];battles:=[]Battle{};for j,p:=range pairs{song:=songs[best.songs[j]];ra,rb:=rank(ds[p[0]],song),rank(ds[p[1]],song);battles=append(battles,Battle{A:ds[p[0]],B:ds[p[1]],Song:song,RankA:ra,RankB:rb});diff:=float64(ra-rb);debt[p[0]]+=diff;debt[p[1]]-=diff};usedMatching[best.matching]=true;usedSong[best.songs[0]]=true;usedSong[best.songs[1]]=true;rounds=append(rounds,Round{Number:roundNo,Battles:battles})};return rounds}
