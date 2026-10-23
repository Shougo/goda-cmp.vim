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

  let was_enabled = exists('s:enabled')
  call goda_cmp#disable()
  " One command, no cd and no &&: the latter differs between shells (cmd.exe,
  " PowerShell, sh). "go -C" runs go in that directory (Go 1.20 or later).
  let out = system('go -C ' . shellescape(dir) . ' build -o ' . shellescape(bin) . ' .')
  let failed = v:shell_error
  if was_enabled
    call goda_cmp#enable()
  endif
  if failed
    throw 'goda-cmp: go build failed:' . "\n" . out
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
  augroup goda_cmp
    autocmd!
    autocmd TextChangedI * call s:on_change('TextChangedI')
    autocmd TextChangedP * call s:on_change('TextChangedP')
    autocmd InsertLeave * call goda_cmp#hide()
  augroup END
  call s:setup_events()
endfunction

function! goda_cmp#disable() abort
  if !exists('s:enabled')
    return
  endif
  unlet s:enabled
  call goda_cmp#disable_cmdline()
  silent! autocmd! goda_cmp
  silent! autocmd! goda_cmp_events
  call goda_cmp#hide()
  call goda#stop('cmp')
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
    " "=": the expression register, where completing is no help.
    autocmd CmdlineChanged * if getcmdtype() !=# '='
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
  call goda_cmp#hide()
  call goda_cmp#disable_cmdline()
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

function! s:merge(base, patch) abort
  let r = deepcopy(a:base)
  for [key, val] in items(a:patch)
    if key ==# 'sourceOptions' && type(val) == v:t_dict
      let opts = get(r, 'sourceOptions', {})
      for [src, opt] in items(val)
        let opts[src] = extend(get(opts, src, {}), deepcopy(opt))
      endfor
      let r.sourceOptions = opts
    else
      let r[key] = deepcopy(val)
    endif
  endfor
  return r
endfunction

function! s:check(config) abort
  if goda#is_running('cmp')
    call goda#request('cmp', 'validate', a:config)
  endif
endfunction

function! goda_cmp#patch_global(patch) abort
  call s:check(a:patch)
  let s:global_patch = s:merge(s:global_patch, a:patch)
endfunction

function! goda_cmp#patch_filetype(filetype, patch) abort
  call s:check(a:patch)
  let s:filetype_patch[a:filetype] = s:merge(get(s:filetype_patch, a:filetype, {}), a:patch)
endfunction

function! goda_cmp#patch_buffer(patch) abort
  call s:check(a:patch)
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
  call s:check(a:config)
  let s:global_patch = deepcopy(a:config)
endfunction

function! goda_cmp#set_filetype(filetype, config) abort
  call s:check(a:config)
  let s:filetype_patch[a:filetype] = deepcopy(a:config)
endfunction

function! goda_cmp#set_buffer(config) abort
  call s:check(a:config)
  let b:goda_cmp_patch = deepcopy(a:config)
endfunction

" What the plugin needs to resolve the settings of this buffer.
function! s:patches() abort
  let fts = {}
  for part in split(&filetype, '\.')
    if has_key(s:filetype_patch, part)
      let fts[part] = s:filetype_patch[part]
    endif
  endfor
  return {'global': s:global_patch, 'filetype': fts, 'buffer': get(b:, 'goda_cmp_patch', {})}
endfunction

" ---------------------------------------------------------------------------
" Requests

function! s:options() abort
  let patches = s:patches()
  let key = &filetype . "\n" . json_encode(patches)
  if !has_key(s:opts, key)
    if len(s:opts) > 50
      let s:opts = {}
    endif
    try
      let s:opts[key] = goda#request('cmp', 'options', &filetype, patches)
    catch
      return {'autoComplete': 0, 'delay': 0}
    endtry
  endif
  return s:opts[key]
endfunction

" Insert mode or, after goda_cmp#enable_cmdline(), the command line.
function! s:cmdline() abort
  return mode() ==# 'c' && exists('s:cmdline')
endfunction

" The text before the cursor: in the buffer, or on the command line.
function! s:input() abort
  return s:cmdline() ? strpart(getcmdline(), 0, getcmdpos() - 1)
        \ : strpart(getline('.'), 0, col('.') - 1)
endfunction

" What identifies a request: the same key twice is not asked again.
function! s:key() abort
  return [bufnr('%'), s:cmdline() ? 'c' : line('.'), s:input()]
endfunction

function! s:context(manual, event) abort
  let s:seq += 1
  let cmdline = s:cmdline()
  return {
        \ 'seq': s:seq,
        \ 'tick': b:changedtick,
        \ 'bufnr': bufnr('%'),
        \ 'lnum': line('.'),
        \ 'col': cmdline ? getcmdpos() : col('.'),
        \ 'input': s:input(),
        \ 'filetype': &filetype,
        \ 'mode': cmdline ? 'c' : 'i',
        \ 'cmdtype': cmdline ? getcmdtype() : '',
        \ 'manual': a:manual ? v:true : v:false,
        \ 'event': a:event,
        \ 'patches': s:patches(),
        \ }
endfunction

function! s:send(manual, event) abort
  let s:timer = -1
  if !(mode() =~# '^i' || s:cmdline()) || !goda#is_running('cmp')
    return
  endif
  call goda#notify('cmp', 'complete', s:context(a:manual, a:event))
endfunction

function! s:on_change(event) abort
  if !goda#is_running('cmp')
    return
  endif
  let opts = s:options()
  " Text that the popup itself inserted (selecting an item) is told apart by
  " the UI, on the Go side (cmp.Skipper).
  if !get(opts, 'autoComplete', 1)
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
  let s:timer = timer_start(get(opts, 'delay', 0), {-> s:send(0, a:event)})
endfunction

" Complete now, whatever the typed length. Meant for a mapping:
"   inoremap <C-n> <Cmd>call goda_cmp#complete()<CR>
function! goda_cmp#complete() abort
  let s:last = []
  let s:accepted = []
  call s:send(1, '')
  return ''
endfunction

function! goda_cmp#hide() abort
  if s:timer != -1
    call timer_stop(s:timer)
    let s:timer = -1
  endif
  let s:last = []
  let s:accepted = []
  if goda#is_running('cmp')
    try
      call goda#notify('cmp', 'hide', &filetype, s:patches())
    catch
    endtry
  endif
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
  if get(a:r, 'mode', 'i') ==# 'c'
    return mode() ==# 'c'
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
  " start another completion for the word that was just completed.
  if s:timer != -1
    call timer_stop(s:timer)
    let s:timer = -1
  endif
  let s:last = s:key()
  let s:accepted = [s:last[0], s:last[2]]
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
