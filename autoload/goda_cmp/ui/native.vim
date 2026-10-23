" The Vim side of the "native" UI (ext/ui/native): Vim's popup menu.
" Called only by that UI; reaches the core through goda_cmp#_* receivers.

" complete() obeys 'completeopt': without noinsert/noselect it inserts the first
" candidate into the text, and with longest the common prefix. Neither is wanted
" for completion while typing, so the options are adjusted for as long as the
" insert mode lasts and put back when it ends. Other options (popup, preview,
" ...) are kept.
let s:saved_completeopt = v:null
let s:selected_ref = ''

" The autocmds are set when the UI is first used.
function! s:setup() abort
  if exists('s:ready')
    return
  endif
  let s:ready = 1
  augroup goda_cmp_ui_native
    autocmd!
    autocmd CompleteDone * call s:on_done(v:completed_item, get(v:event, 'reason', 'accept'))
    autocmd CompleteChanged * call s:on_select(get(v:event, 'completed_item', {}))
    autocmd InsertLeave * call s:restore_completeopt()
  augroup END
endfunction

function! s:on_done(item, reason) abort
  let s:selected_ref = ''
  call goda_cmp#_done(a:item, a:reason)
endfunction

function! s:on_select(item) abort
  let ref = type(a:item) == v:t_dict ? get(a:item, 'user_data', '') : ''
  let s:selected_ref = type(ref) == v:t_string ? ref : ''
  call goda_cmp#_select(a:item)
endfunction

" INSERT (the "insert" param): insert the first item.
function! s:set_completeopt(insert) abort
  if s:saved_completeopt is v:null
    let s:saved_completeopt = &completeopt
  endif
  let keep = filter(split(s:saved_completeopt, ','),
        \ {_, v -> index(['longest', 'noinsert', 'noselect', 'menuone'], v) < 0})
  let &completeopt = join(keep + (a:insert ? ['menuone'] : ['menuone', 'noinsert', 'noselect']), ',')
endfunction

function! s:restore_completeopt() abort
  if s:saved_completeopt isnot v:null
    let &completeopt = s:saved_completeopt
    let s:saved_completeopt = v:null
  endif
endfunction

function! goda_cmp#ui#native#show(r) abort
  call s:setup()
  if !goda_cmp#_fresh(a:r)
    return 0
  endif
  " While sources are still answering, do not take the popup away from a user
  " who is choosing: complete() would drop the selection.
  if !a:r.final && get(complete_info(['selected']), 'selected', -1) >= 0
    return 0
  endif
  call s:set_completeopt(get(a:r, 'insert', v:false))
  call complete(a:r.col, a:r.items)
  return 1
endfunction

function! goda_cmp#ui#native#hide() abort
  if mode() =~# '^i' && pumvisible()
    call complete(col('.'), [])
  endif
endfunction

" The resolved info of the selected item: only possible when 'completeopt'
" has "popup" and the item had an info, so that Vim opened the info popup.
function! goda_cmp#ui#native#update_info(r) abort
  if a:r.ref !=# s:selected_ref || !exists('*popup_findinfo')
    return 0
  endif
  let id = popup_findinfo()
  if id <= 0
    return 0
  endif
  call popup_settext(id, split(a:r.info, "\n"))
  return 1
endfunction
