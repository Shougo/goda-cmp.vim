" The Vim side of the "pum" UI (ext/ui/pum): pum.vim. Called only by that
" UI; reaches the core through goda_cmp#_* receivers.

let s:selected_ref = ''

" The autocmds are set when the UI is first used.
function! s:setup() abort
  if exists('s:ready')
    return
  endif
  let s:ready = 1
  augroup goda_cmp_ui_pum
    autocmd!
    autocmd User PumCompleteDone call s:on_done(get(g:, 'pum#completed_item', {}))
    autocmd User PumCompleteChanged call s:on_select(exists('*pum#current_item') ? pum#current_item() : {})
  augroup END
endfunction

function! s:on_done(item) abort
  let s:selected_ref = ''
  call goda_cmp#_done(a:item, 'accept')
endfunction

function! s:on_select(item) abort
  let ref = type(a:item) == v:t_dict ? get(a:item, 'user_data', '') : ''
  let s:selected_ref = type(ref) == v:t_string ? ref : ''
  call goda_cmp#_select(a:item)
endfunction

function! goda_cmp#ui#pum#show(r) abort
  call s:setup()
  if !goda_cmp#_fresh(a:r)
    return 0
  endif
  call pum#open(a:r.col, a:r.items)
  return 1
endfunction

function! goda_cmp#ui#pum#hide() abort
  if exists('*pum#visible') && pum#visible()
    call pum#close()
  endif
endfunction

" Did pum.vim change the text itself (inserting the selected item)?
function! goda_cmp#ui#pum#skip() abort
  return exists('*pum#skip_complete') && pum#skip_complete()
endfunction

function! goda_cmp#ui#pum#update_info(r) abort
  if a:r.ref !=# s:selected_ref || !exists('*pum#visible') || !pum#visible()
    return 0
  endif
  call pum#update_current_item({'info': a:r.info})
  return 1
endfunction
