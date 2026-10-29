" goda-cmp.vim - completion for Vim/Neovim on goda.vim
"
" The Go side (the user's own build of config.go, see README) gathers, filters
" and shows candidates; this file collects the context while typing and passes
" on what the UI reports. It knows no extension: UIs and sources call the
" editor themselves, and their Vim script (autoload/goda_cmp/ui/*.vim, for
" instance) calls the goda_cmp#_* receivers below. Nothing here calls theirs.

let s:seq = 0
let s:timer = -1
let s:last = []        " [bufnr, lnum, input] of the last request ("c" for lnum
                       " on the command line)
" [bufnr, input] when a candidate was just accepted: no new completion for the
" same text, even when the line moved (an import added above it, say).
let s:accepted = []
let s:opts = {}        " answers of cmp.options, by filetype and patches

" Settings made at run time. They live here, not in the plugin binary: they
" survive a restart of the binary, buffer settings go away with the buffer, and
" they can be made before the binary is started. Every request carries them.
let s:global_patch = {}
let s:filetype_patch = {}

" ---------------------------------------------------------------------------
" Setup

" The binary is machine specific, so it is not built into the configuration
" directory (which belongs in your dotfiles) but into a cache directory.
function! s:cache_dir() abort
  if has('nvim')
    let base = stdpath('cache')
  elseif !empty($XDG_CACHE_HOME)
    let base = $XDG_CACHE_HOME . '/vim'
  elseif has('win32') && !empty($LOCALAPPDATA)
    let base = $LOCALAPPDATA . '/vim'
  else
    let base = expand('~/.cache/vim')
  endif
  return base . '/goda-cmp'
endfunction

function! s:default_bin() abort
  if exists('g:goda_cmp#binary')
    return expand(g:goda_cmp#binary)
  endif
  return s:cache_dir() . '/goda-cmp' . (has('win32') ? '.exe' : '')
endfunction

function! s:config_dir() abort
  if exists('g:goda_cmp#config_dir')
    return expand(g:goda_cmp#config_dir)
  endif
  return (has('nvim') ? stdpath('config') : expand('~/.vim')) . '/goda-cmp'
endfunction

" Build the user's config (a Go module in g:goda_cmp#config_dir). Needs Go.
"
" "go mod tidy" comes first, so that what a "go get" left out of go.sum (the
" requirements of a new extension) is there. It needs the network for what is
" not in the module cache yet, and may fail where the build would not
" (offline, for the tests of a dependency): its failure is told only when
" the build fails too.
"
" If completion is running it is stopped first and started again afterwards
" with the new binary, so this is also what to run after the configuration or
" an extension changed (see the README for an autocmd). When the build fails,
" the previous binary keeps running.
function! goda_cmp#build() abort
  let dir = s:config_dir()
  if !isdirectory(dir)
    throw 'goda-cmp: no config directory: ' . dir
  endif
  let bin = s:default_bin()
  call mkdir(fnamemodify(bin, ':h'), 'p')

  " One command, no cd and no &&: the latter differs between shells (cmd.exe,
  " PowerShell, sh). "go -C" runs go in that directory (Go 1.20 or later).
  " Completion still runs meanwhile.
  let tidy = system('go -C ' . shellescape(dir) . ' mod tidy')
  let tidy_failed = v:shell_error

  let was_enabled = exists('s:enabled')
  call goda_cmp#disable()
  let out = system('go -C ' . shellescape(dir) . ' build -o ' . shellescape(bin) . ' .')
  let failed = v:shell_error
  if was_enabled
    call goda_cmp#enable()
  endif
  if failed
    throw 'goda-cmp: go build failed:' . "\n" . out
          \ . (tidy_failed ? "\n" . 'go mod tidy failed too:' . "\n" . tidy : '')
  endif
  echomsg 'goda-cmp: built ' . bin
endfunction

" Start the plugin binary and begin completing in insert mode.
function! goda_cmp#enable() abort
  if exists('s:enabled')
    return
  endif
  if !executable(s:default_bin())
    " Not an error: this is what the first start looks like.
    echohl WarningMsg
    echomsg 'goda-cmp: ' . s:default_bin() . ' does not exist yet; build it with: call goda_cmp#build()'
    echohl None
    return
  endif
  let plugins = goda#start(s:default_bin())
  if index(plugins, 'cmp') < 0
    " Stop only the binary just started (through one of its plugins), not
    " the binaries of other plugins.
    if !empty(plugins)
      call goda#stop(plugins[0])
    endif
    throw 'goda-cmp: ' . s:default_bin() . ' does not contain the "cmp" plugin'
  endif
  " A binary built from an invalid configuration starts anyway and says why.
  try
    let status = goda#request('cmp', 'status')
  catch
    let status = ''  " a binary from before cmp.status
  endtry
  if type(status) == v:t_string && status !=# ''
    call goda#stop('cmp')
    throw 'goda-cmp: fix the configuration in ' . s:config_dir()
          \ . ' and run goda_cmp#build(): ' . status
  endif
  let s:enabled = 1
  let s:registered = {}
  let s:opts = {}
  augroup goda_cmp
    autocmd!
    autocmd InsertLeave * call goda_cmp#hide('InsertLeave')
    " The events of autoCompleteEvents are listened to as the settings of
    " a buffer become known.
    autocmd BufEnter,FileType * call s:register_events()
    " input() and the expression register are completed without
    " goda_cmp#enable_cmdline(), as in ddc.vim (with their sources in
    " cmdlineSources["@"] and ["="]).
    autocmd CmdlineChanged * if s:input_cmdline()
          \ | call s:on_change('CmdlineChanged') | endif
    autocmd CmdlineLeave * if s:input_cmdline()
          \ | call goda_cmp#hide('CmdlineLeave') | endif
  augroup END
  call s:register_events()
  call s:setup_events()
endfunction

function! goda_cmp#disable() abort
  if !exists('s:enabled')
    return
  endif
  unlet s:enabled
  call goda_cmp#disable_cmdline()
  call goda_cmp#disable_terminal()
  silent! autocmd! goda_cmp
  silent! autocmd! goda_cmp_events
  call goda_cmp#hide()
  call goda#stop('cmp')
endfunction

" The events that complete (Config.AutoCompleteEvents). The command-line and
" terminal events have their own groups, set by goda_cmp#enable_cmdline() and
" goda_cmp#enable_terminal().
let s:own_events = ['CmdlineEnter', 'CmdlineChanged', 'CmdlineLeave', 'TextChangedT']

function! s:register_events() abort
  if !exists('s:enabled') || !goda#is_running('cmp')
    return
  endif
  for event in get(s:options(), 'autoCompleteEvents', [])
    if has_key(s:registered, event) || index(s:own_events, event) >= 0
      continue
    endif
    let s:registered[event] = 1
    if !exists('##' . event)
      echohl WarningMsg
      echomsg 'goda-cmp: unknown autocmd event in autoCompleteEvents: ' . event
      echohl None
      continue
    endif
    execute 'autocmd goda_cmp' event '* call s:on_change(' string(event) ')'
  endfor
endfunction

" ---------------------------------------------------------------------------
" Command-line completion, as ddc#enable_cmdline_completion(): enabled for the
" next command line only, for example from the mapping that opens it:
"   nnoremap : <Cmd>call goda_cmp#enable_cmdline()<CR>:
" The sources are Config.CmdlineSources of the command-line type, and the UI
" must be able to show them there (pum).

function! goda_cmp#enable_cmdline() abort
  call goda_cmp#enable()
  if !exists('s:enabled')
    return
  endif
  let s:cmdline = 1
  augroup goda_cmp_cmdline
    autocmd!
    autocmd CmdlineEnter * call s:on_change('CmdlineEnter')
    " input() and the expression register: see goda_cmp#enable().
    autocmd CmdlineChanged * if getcmdtype() !~# '^[@=]$'
          \ | call s:on_change('CmdlineChanged') | endif
    autocmd CmdlineLeave * call s:cmdline_leave()
  augroup END
endfunction

function! goda_cmp#disable_cmdline() abort
  if !exists('s:cmdline')
    return
  endif
  unlet s:cmdline
  silent! autocmd! goda_cmp_cmdline
  if exists('#User#GodaCmpCmdlineLeave')
    doautocmd <nomodeline> User GodaCmpCmdlineLeave
  endif
endfunction

function! s:cmdline_leave() abort
  call goda_cmp#hide('CmdlineLeave')
  call goda_cmp#disable_cmdline()
endfunction

" ---------------------------------------------------------------------------
" Terminal completion, as ddc#enable_terminal_completion(): typing in a
" terminal buffer completes, with a UI that can show candidates there (pum).
" Terminal buffers are special buffers: set specialBufferCompletion for them.

function! goda_cmp#enable_terminal() abort
  call goda_cmp#enable()
  if !exists('s:enabled')
    return
  endif
  if !exists('##TextChangedT')
    throw 'goda-cmp: terminal completion needs the TextChangedT event (Vim 9.0.1513 or Neovim 0.9)'
  endif
  let s:terminal = 1
  augroup goda_cmp_terminal
    autocmd!
    autocmd TextChangedT * call s:on_change('TextChangedT')
    if exists('##ModeChanged')
      autocmd ModeChanged t:* call goda_cmp#hide('ModeChanged')
    endif
  augroup END
endfunction

function! goda_cmp#disable_terminal() abort
  if !exists('s:terminal')
    return
  endif
  unlet s:terminal
  silent! autocmd! goda_cmp_terminal
endfunction

" ---------------------------------------------------------------------------
" Buffer events for the sources that want them (cmp.EventHandler). The plugin
" says which autocmd events for which filetypes; buffers of other filetypes
" cost one comparison here and nothing else.

let s:event_seq = 0
let s:event_fts = {}  " event -> filetypes ([] for all)

function! s:setup_events() abort
  silent! autocmd! goda_cmp_events
  let s:event_fts = {}
  " A new binary expects the numbering to start at 1.
  let s:event_seq = 0
  try
    let table = goda#request('cmp', 'events')
  catch
    " an older binary without events
    return
  endtry
  augroup goda_cmp_events
    for e in table
      if !exists('##' . e.event)
        echohl WarningMsg
        echomsg 'goda-cmp: unknown autocmd event: ' . e.event
        echohl None
        continue
      endif
      let s:event_fts[e.event] = e.filetypes
      execute 'autocmd' e.event '* call s:on_event(' string(e.event) ', str2nr(expand("<abuf>")))'
    endfor
  augroup END
  " The current buffer was entered before the plugin started.
  if has_key(s:event_fts, 'BufEnter')
    call s:on_event('BufEnter', bufnr('%'))
  endif
endfunction

function! s:on_event(event, bufnr) abort
  let ft = getbufvar(a:bufnr, '&filetype')
  let fts = get(s:event_fts, a:event, [])
  if !empty(fts) && empty(filter(split(ft, '\.'), {_, v -> index(fts, v) >= 0}))
    return
  endif
  if !goda#is_running('cmp')
    return
  endif
  let name = bufname(a:bufnr)
  let s:event_seq += 1
  call goda#notify('cmp', 'event', {
        \ 'seq': s:event_seq,
        \ 'name': a:event,
        \ 'bufnr': a:bufnr,
        \ 'tick': getbufvar(a:bufnr, 'changedtick'),
        \ 'filetype': ft,
        \ 'buftype': getbufvar(a:bufnr, '&buftype'),
        \ 'path': name ==# '' ? '' : fnamemodify(name, ':p'),
        \ })
endfunction

" ---------------------------------------------------------------------------
" Settings (ddc.vim: ddc#custom#patch_*). A patch is a dictionary with the
" keys of cmp.Config (ui, sources, sourceOptions, ...). Maps are merged by
" key, everything else is replaced. A patch is checked by the plugin when it
" is running, and an invalid one throws.

" The keys whose values are merged by key; for the first ones, the value of
" each key is merged by key too.
let s:nested_keys = ['sourceOptions', 'sourceParams', 'filterParams', 'uiParams']
let s:map_keys = ['cmdlineSources']

function! s:merge(base, patch) abort
  let r = deepcopy(a:base)
  for [key, val] in items(a:patch)
    if index(s:nested_keys, key) >= 0 && type(val) == v:t_dict
      let m = get(r, key, {})
      for [name, v] in items(val)
        let m[name] = type(v) == v:t_dict
              \ ? extend(get(m, name, {}), deepcopy(v)) : deepcopy(v)
      endfor
      let r[key] = m
    elseif index(s:map_keys, key) >= 0 && type(val) == v:t_dict
      let r[key] = extend(get(r, key, {}), deepcopy(val))
    else
      let r[key] = deepcopy(val)
    endif
  endfor
  return r
endfunction

function! s:check(config, scope) abort
  if goda#is_running('cmp')
    call goda#request('cmp', 'validate', a:config, s:aliases, a:scope)
  endif
endfunction

function! goda_cmp#patch_global(patch) abort
  call s:check(a:patch, 'global')
  let s:global_patch = s:merge(s:global_patch, a:patch)
endfunction

function! goda_cmp#patch_filetype(filetype, patch) abort
  for ft in type(a:filetype) == v:t_list ? a:filetype : [a:filetype]
    call s:check(a:patch, 'filetype')
    let s:filetype_patch[ft] = s:merge(get(s:filetype_patch, ft, {}), a:patch)
  endfor
endfunction

function! goda_cmp#patch_buffer(patch) abort
  call s:check(a:patch, 'buffer')
  let b:goda_cmp_patch = s:merge(get(b:, 'goda_cmp_patch', {}), a:patch)
endfunction

function! goda_cmp#get_global() abort
  return deepcopy(s:global_patch)
endfunction

function! goda_cmp#get_filetype(filetype) abort
  return deepcopy(get(s:filetype_patch, a:filetype, {}))
endfunction

function! goda_cmp#get_buffer() abort
  return deepcopy(get(b:, 'goda_cmp_patch', {}))
endfunction

function! goda_cmp#set_global(config) abort
  call s:check(a:config, 'global')
  let s:global_patch = deepcopy(a:config)
endfunction

function! goda_cmp#set_filetype(filetype, config) abort
  for ft in type(a:filetype) == v:t_list ? a:filetype : [a:filetype]
    call s:check(a:config, 'filetype')
    let s:filetype_patch[ft] = deepcopy(a:config)
  endfor
endfunction

function! goda_cmp#set_buffer(config) abort
  call s:check(a:config, 'buffer')
  let b:goda_cmp_patch = deepcopy(a:config)
endfunction

" The configuration in effect for the current buffer (Go and Vim script
" settings together), as ddc#custom#get_current().
function! goda_cmp#get_current() abort
  return goda#request('cmp', 'current', s:filetype(), s:patches())
endfunction

" Give the extension {base} of {type} ("source", "filter" or "ui") another
" name, under which it can have options and params of its own, as
" ddc#custom#alias().
function! goda_cmp#alias(type, alias, base) abort
  if index(['source', 'filter', 'ui'], a:type) < 0
    throw 'goda-cmp: the type of an alias is "source", "filter" or "ui", not ' . string(a:type)
  endif
  let aliases = deepcopy(s:aliases)
  let aliases[a:type][a:alias] = a:base
  if goda#is_running('cmp')
    call goda#request('cmp', 'validate', {}, aliases, 'global')
  endif
  let s:aliases = aliases
  let s:opts = {}
endfunction

let s:aliases = {'source': {}, 'filter': {}, 'ui': {}}

" What the plugin needs to resolve the settings of this buffer.
function! s:patches(...) abort
  let ft = a:0 ? a:1 : s:filetype()
  let fts = {}
  for part in split(ft, '\.')
    if has_key(s:filetype_patch, part)
      let fts[part] = s:filetype_patch[part]
    endif
  endfor
  return {'global': s:global_patch, 'filetype': fts,
        \ 'buffer': get(b:, 'goda_cmp_patch', {}), 'aliases': s:aliases}
endfunction

" ---------------------------------------------------------------------------
" Requests

function! s:request_options(filetype, patches) abort
  let key = a:filetype . "\n" . json_encode(a:patches)
  if !has_key(s:opts, key)
    if len(s:opts) > 50
      let s:opts = {}
    endif
    try
      let s:opts[key] = goda#request('cmp', 'options', a:filetype, a:patches)
    catch
      return {'autoComplete': 0, 'delay': 0}
    endtry
  endif
  return s:opts[key]
endfunction

function! s:options() abort
  let ft = s:filetype()
  return s:request_options(ft, s:patches(ft))
endfunction

" The filetype of a request: 'filetype', or with contextFiletype (set
" globally or for the buffer, so known without a filetype) the language at the
" cursor.
function! s:filetype() abort
  if !goda#is_running('cmp')
    return &filetype
  endif
  let patches = {'global': s:global_patch, 'filetype': {},
        \ 'buffer': get(b:, 'goda_cmp_patch', {}), 'aliases': s:aliases}
  let how = get(s:request_options('', patches), 'contextFiletype', 'none')
  if how ==# 'context_filetype'
    if exists('*context_filetype#get_filetype')
      let ft = context_filetype#get_filetype()
      if ft !=# ''
        return ft
      endif
    endif
  elseif how ==# 'treesitter'
    let ft = goda_cmp#syntax#lang()
    if ft !=# ''
      return ft
    endif
  endif
  return &filetype
endfunction

" Insert mode, the command line (after goda_cmp#enable_cmdline()) or a
" terminal (after goda_cmp#enable_terminal()): "i", "c" or "t".
function! s:mode() abort
  let m = mode()
  if m ==# 'c'
    return exists('s:cmdline') || getcmdtype() =~# '^[@=]$' ? 'c' : ''
  elseif m ==# 't'
    return exists('s:terminal') ? 't' : ''
  endif
  return m =~# '^i' ? 'i' : ''
endfunction

" input() or the expression register, outside goda_cmp#enable_cmdline().
function! s:input_cmdline() abort
  return !exists('s:cmdline') && getcmdtype() =~# '^[@=]$'
endfunction

function! s:cmdline() abort
  return s:mode() ==# 'c'
endfunction

" The cursor column (byte, 1-based) and the text of its line: in the buffer,
" on the command line, or in a terminal (Vim keeps the terminal's cursor apart
" from the buffer's).
function! s:col() abort
  let m = s:mode()
  if m ==# 't' && !has('nvim')
    return term_getcursor(bufnr('%'))[1]
  endif
  return m ==# 'c' ? getcmdpos() : col('.')
endfunction

function! s:line() abort
  let m = s:mode()
  if m ==# 'c'
    return getcmdline()
  elseif m ==# 't' && !has('nvim')
    return term_getline('', term_getcursor(bufnr('%'))[0])
  endif
  return getline('.')
endfunction

" The text before the cursor.
function! s:input() abort
  if s:mode() ==# 't' && !has('nvim')
    " The terminal's cursor is a screen column.
    let text = s:line()
    let col = s:col()
    return col > strdisplaywidth(text) ? text : matchstr(text, '^.*\%<' . col . 'v')
  endif
  return strpart(s:line(), 0, s:col() - 1)
endfunction

function! s:lnum() abort
  if s:mode() ==# 't' && !has('nvim')
    let rows = term_getsize(bufnr('%'))[0]
    return max([1, line('$') - rows + term_getcursor(bufnr('%'))[0]])
  endif
  return line('.')
endfunction

" What identifies a request: the same key twice is not asked again.
function! s:key() abort
  return [bufnr('%'), s:cmdline() ? 'c' : s:lnum(), s:input()]
endfunction

function! s:context(manual, event) abort
  let s:seq += 1
  let mode = s:mode()
  let ft = s:filetype()
  return {
        \ 'seq': s:seq,
        \ 'tick': b:changedtick,
        \ 'bufnr': bufnr('%'),
        \ 'lnum': s:lnum(),
        \ 'col': s:col(),
        \ 'input': s:input(),
        \ 'nextinput': goda_cmp#_next_input(),
        \ 'filetype': ft,
        \ 'mode': mode ==# '' ? 'i' : mode,
        \ 'cmdtype': mode ==# 'c' ? getcmdtype() : '',
        \ 'manual': a:manual ? v:true : v:false,
        \ 'event': a:event,
        \ 'patches': s:patches(ft),
        \ }
endfunction

" EXTRA: more keys of the request ("options", "hide", "update").
function! s:send(manual, event, ...) abort
  let s:timer = -1
  if s:mode() ==# '' || !goda#is_running('cmp')
    return
  endif
  call goda#notify('cmp', 'complete', extend(s:context(a:manual, a:event), a:0 ? a:1 : {}))
endfunction

" Changes that are no typing: completion is skipped for them, as in ddc.vim.
function! s:skipped() abort
  if &paste || (s:mode() ==# 'i' && &iminsert == 1)
    return 1
  endif
  return s:mode() ==# 'c' && wildmenumode()
endfunction

let s:prev_input = ''
let s:skip_next = 0

function! s:on_change(event) abort
  if !goda#is_running('cmp') || s:mode() ==# ''
    return
  endif
  let input = s:input()
  let prev = s:prev_input
  let s:prev_input = input
  if s:skip_next > 0
    let s:skip_next -= 1
    return
  endif
  if s:skipped()
    return
  endif
  let opts = s:options()
  " Text that the popup itself inserted (selecting an item) is told apart by
  " the UI, on the Go side (cmp.Skipper).
  if !get(opts, 'autoComplete', 1)
        \ || index(get(opts, 'autoCompleteEvents', [a:event]), a:event) < 0
    return
  endif
  if s:mode() !=# 'c' && &buftype !=# '' && !get(opts, 'specialBufferCompletion', 0)
    return
  endif
  " A backspace closes the popup instead of completing again (less
  " flicker), unless backspaceCompletion is set.
  if !get(opts, 'backspaceCompletion', 0) && input !=# prev
        \ && strlen(input) < strlen(prev) && stridx(prev, input) == 0
        \ && strchars(prev) == strchars(input) + 1
    call goda_cmp#hide('Backspace')
    let s:last = s:key()
    return
  endif
  let key = s:key()
  if key ==# s:last
    return
  endif
  if !empty(s:accepted)
    if [key[0], key[2]] ==# s:accepted
      return
    endif
    let s:accepted = []
  endif
  let s:last = key

  if s:timer != -1
    call timer_stop(s:timer)
  endif
  let extra = get(opts, 'hideOnEvents', 0) ? {'hide': v:true} : {}
  let s:timer = timer_start(get(opts, 'delay', 0), {-> s:send(0, a:event, extra)})
endfunction

" Complete now, whatever the typed length. Meant for a mapping:
"   inoremap <C-n> <Cmd>call goda_cmp#complete()<CR>
" {options} is a patch for this request (and the narrowing that follows), as
" ddc#map#manual_complete({options}).
function! goda_cmp#complete(options = {}) abort
  let s:last = []
  let s:accepted = []
  call s:send(1, 'Manual', empty(a:options) ? {} : {'options': a:options})
  return ''
endfunction

function! goda_cmp#hide(event = 'Manual') abort
  if s:timer != -1
    call timer_stop(s:timer)
    let s:timer = -1
  endif
  let s:last = []
  let s:accepted = []
  let s:items = {}
  if goda#is_running('cmp')
    try
      let ft = s:filetype()
      call goda#notify('cmp', 'hide', ft, s:patches(ft))
    catch
    endtry
  endif
endfunction

" Is the popup open? Asks the UI (cmp.Visibler).
function! goda_cmp#visible() abort
  if !exists('s:enabled') || !goda#is_running('cmp')
    return v:false
  endif
  try
    let ft = s:filetype()
    return goda#request('cmp', 'visible', ft, s:patches(ft)) ? v:true : v:false
  catch
    return v:false
  endtry
endfunction

" Skip the next automatic completion.
function! goda_cmp#skip_next_complete() abort
  let s:skip_next += 1
endfunction

" Act as if the autocmd {event} happened: the sources that listen to it hear
" about it, and it completes when it is in autoCompleteEvents.
function! goda_cmp#on_event(event) abort
  if !exists('s:enabled') || !goda#is_running('cmp')
    return
  endif
  if has_key(s:event_fts, a:event)
    call s:on_event(a:event, bufnr('%'))
  endif
  call s:on_change(a:event)
endfunction

" For UIs not of goda-cmp: the user accepted {item}.
function! goda_cmp#on_complete_done(item) abort
  call goda_cmp#_done(a:item, 'accept')
endfunction

" Replace the items gathered last by the source {name} with {items} (a list of
" complete-items) and show the list again, as ddc#update_items(). Nothing
" happens when the source has not gathered for this word.
function! goda_cmp#update_items(name, items) abort
  call s:send(0, 'Update', {'update': {'name': a:name, 'items': a:items}})
endfunction

" What the source of ITEM shows for it in a preview window, in ddc.vim's
" previewer format ({'kind': 'empty'} when it shows nothing, for instance for
" an item that is not goda-cmp's). For pum.vim:
"   call pum#set_option('previewer', 'goda_cmp#get_previewer')
function! goda_cmp#get_previewer(item) abort
  let ref = s:ref(a:item)
  if ref ==# '' || !exists('s:enabled') || !goda#is_running('cmp')
    return {'kind': 'empty'}
  endif
  try
    let previewer = goda#request('cmp', 'preview', ref)
  catch
    " An old list, a slow source: there is just no preview.
    return {'kind': 'empty'}
  endtry
  return type(previewer) == v:t_dict ? previewer : {'kind': 'empty'}
endfunction

" ---------------------------------------------------------------------------
" What the engine shows (it calls goda_cmp#_items(), this file's own), for the
" goda_cmp#map#*() functions.

let s:items = {}

function! goda_cmp#_items(r) abort
  let s:items = a:r.seq == s:seq ? a:r : {}
endfunction

" {items, col, sources} of the list shown, or {} when there is none.
function! goda_cmp#_shown() abort
  if empty(s:items) || s:items.seq != s:seq || empty(s:items.items)
    return {}
  endif
  return s:items
endfunction

" The text before the cursor, for goda_cmp#map#*() and the Vim script of
" extensions.
function! goda_cmp#_input() abort
  return s:input()
endfunction

" The text after the cursor.
function! goda_cmp#_next_input() abort
  return s:line()[strlen(s:input()) :]
endfunction

" The whole line: of the buffer, the command line or the terminal.
function! goda_cmp#_line() abort
  return s:line()
endfunction

function! goda_cmp#_col() abort
  return s:col()
endfunction

" ---------------------------------------------------------------------------
" Receivers for the Vim script of UIs. They are the only way an extension's
" Vim script reaches the core.

" Is a list the Go side asked to show still wanted? It is not when a newer
" request was made or the text changed in the meantime.
function! goda_cmp#_fresh(r) abort
  if !exists('s:enabled') || a:r.seq != s:seq
    return 0
  endif
  let mode = get(a:r, 'mode', 'i')
  if mode ==# 'c'
    return mode() ==# 'c'
  elseif mode ==# 't'
    " A terminal changes on its own: only the mode is checked.
    return mode() ==# 't'
  endif
  return a:r.tick == b:changedtick && mode() =~# '^i'
endfunction

" The user_data of a candidate is a reference the plugin gave it (see
" Item.Ref); a source's OnCompleteDone and Resolve are reached through it.
function! s:ref(item) abort
  if type(a:item) != v:t_dict
    return ''
  endif
  let ref = get(a:item, 'user_data', '')
  return type(ref) == v:t_string ? ref : ''
endfunction

" The user accepted ITEM (REASON "cancel": the popup was dismissed instead).
" Items that are not goda-cmp's are ignored.
function! goda_cmp#_done(item, reason) abort
  let ref = s:ref(a:item)
  if ref ==# '' || !exists('s:enabled')
    return
  endif
  " Accepting inserted nothing new: the TextChangedI that may follow must not
  " start another completion for the word that was just completed. But a UI
  " may tell it late, after more was typed (pum.vim closing its popup on the
  " next key, "/" after a directory): what was typed since is completed.
  let word = get(a:item, 'word', '')
  let input = s:input()
  if type(word) != v:t_string || strpart(input, len(input) - len(word)) ==# word
    if s:timer != -1
      call timer_stop(s:timer)
      let s:timer = -1
    endif
    let s:last = s:key()
    let s:accepted = [s:last[0], s:last[2]]
    let s:prev_input = s:last[2]
    let s:items = {}
  endif
  if a:reason !=# 'cancel' && goda#is_running('cmp')
    call goda#notify('cmp', 'done', ref)
  endif
endfunction

" The user moved to ITEM: its source may resolve it (LSP
" completionItem/resolve), and the UI is told the result (cmp.InfoUpdater).
function! goda_cmp#_select(item) abort
  let ref = s:ref(a:item)
  if ref !=# '' && exists('s:enabled') && goda#is_running('cmp')
    call goda#notify('cmp', 'select', ref)
  endif
endfunction
