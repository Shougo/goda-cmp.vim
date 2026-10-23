" Functions for mappings, as ddc#map#*(). The ones that return keys are meant
" for <expr> mappings.

" Show the list shown last again, with the UI {ui}.
"   inoremap <expr> <C-x><C-p> goda_cmp#map#complete('pum')
function! goda_cmp#map#complete(ui) abort
  if goda#is_running('cmp')
    call goda#notify('cmp', 'show', a:ui)
  endif
  return ''
endfunction

" Complete now (goda_cmp#complete()), with {options} for this request.
function! goda_cmp#map#manual_complete(options = {}) abort
  return goda_cmp#complete(a:options)
endfunction

" Is there a list to complete from?
function! goda_cmp#map#can_complete() abort
  let r = goda_cmp#_shown()
  return !empty(r) && r.col > 0 ? v:true : v:false
endfunction

" Accept with {confirm_key} and complete again with the same sources.
function! goda_cmp#map#extend(confirm_key) abort
  let r = goda_cmp#_shown()
  if empty(r)
    return ''
  endif
  let sources = r.sources
  return a:confirm_key .. "\<Cmd>call goda_cmp#complete("
        \ .. string({'sources': sources}) .. ")\<CR>"
endfunction

" Insert the longest common prefix of the items (ignoring case).
function! goda_cmp#map#complete_common_string() abort
  let r = goda_cmp#_shown()
  if empty(r)
    return ''
  endif
  let complete_str = goda_cmp#_input()[r.col - 1 :]
  let common = r.items[0].word
  for item in r.items[1:]
    while stridx(tolower(item.word), tolower(common)) != 0
      let common = strcharpart(common, 0, strchars(common) - 1)
    endwhile
  endfor
  if common ==# '' || complete_str ==? common
    return ''
  endif
  return s:replace(complete_str, common)
endfunction

" Insert the item at {index} (from 0) of the list.
function! goda_cmp#map#insert_item(index) abort
  let r = goda_cmp#_shown()
  if empty(r)
    return ''
  endif
  let item = get(r.items, a:index, {})
  if get(item, 'word', '') ==# ''
    return ''
  endif
  let complete_str = goda_cmp#_input()[r.col - 1 :]
  let s:done_item = item
  call goda_cmp#hide('CompleteDone')
  " The text inserted is not typing.
  call goda_cmp#skip_next_complete()
  if stridx(item.word, complete_str) == 0
    let chars = item.word[len(complete_str):]
  else
    let chars = s:replace(complete_str, item.word)
  endif
  " The source hears about it after the text is in.
  return chars .. "\<Cmd>call goda_cmp#map#_inserted()\<CR>"
endfunction

function! goda_cmp#map#_inserted() abort
  if exists('s:done_item')
    let item = s:done_item
    unlet s:done_item
    call goda_cmp#_done(item, 'accept')
  endif
endfunction

" The keys that replace {from} before the cursor with {to}.
function! s:replace(from, to) abort
  " A new undo point.
  if &l:undolevels > 0
    let &l:undolevels = &l:undolevels
  endif
  let chars = ''
  " 'backspace' must allow deleting what was there before insert mode.
  if mode() ==# 'i'
    let chars .= "\<Cmd>set backspace=start\<CR>"
  endif
  let chars .= repeat("\<BS>", strchars(a:from))
  let chars .= a:to
  if mode() ==# 'i'
    let chars .= printf("\<Cmd>set backspace=%s\<CR>", &backspace)
  endif
  return chars
endfunction
