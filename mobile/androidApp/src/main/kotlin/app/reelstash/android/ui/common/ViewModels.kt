package app.reelstash.android.ui.common

import androidx.compose.runtime.Composable
import androidx.compose.runtime.getValue
import androidx.lifecycle.ViewModel
import androidx.lifecycle.compose.collectAsStateWithLifecycle
import androidx.lifecycle.viewModelScope
import androidx.lifecycle.viewmodel.compose.viewModel
import app.reelstash.shared.model.Page
import kotlinx.coroutines.Job
import kotlinx.coroutines.flow.MutableStateFlow
import kotlinx.coroutines.flow.StateFlow
import kotlinx.coroutines.flow.asStateFlow
import kotlinx.coroutines.launch

/** Loads one value and keeps it across configuration changes. */
class LoadViewModel<T>(private val fetch: suspend () -> T) : ViewModel() {
    private val _state = MutableStateFlow<Load<T>>(Load.Loading)
    val state: StateFlow<Load<T>> = _state.asStateFlow()
    private var job: Job? = null

    init {
        refresh()
    }

    fun refresh(quiet: Boolean = false) {
        job?.cancel()
        if (!quiet || _state.value !is Load.Ready) _state.value = Load.Loading
        job = viewModelScope.launch {
            val result = load { fetch() }
            // A quiet refresh keeps what is on screen when it fails.
            if (!(quiet && result is Load.Failed && _state.value is Load.Ready)) _state.value = result
        }
    }

    fun set(value: T) {
        _state.value = Load.Ready(value)
    }
}

class Loaded<T>(val state: Load<T>, val vm: LoadViewModel<T>) {
    fun retry() = vm.refresh()
    fun refreshQuietly() = vm.refresh(quiet = true)
}

/** A [LoadViewModel] scoped to the current screen; [key] must identify what is loaded. */
@Composable
fun <T> rememberLoad(key: String, fetch: suspend () -> T): Loaded<T> {
    val vm: LoadViewModel<T> = viewModel(key = key) { LoadViewModel(fetch) }
    val state by vm.state.collectAsStateWithLifecycle()
    return Loaded(state, vm)
}

data class PagedState<T>(
    val items: List<T> = emptyList(),
    val total: Int = -1,
    val loading: Boolean = false,
    val error: String? = null,
) {
    val endReached: Boolean get() = total >= 0 && items.size >= total
    val initialLoading: Boolean get() = loading && items.isEmpty()
}

/** An endless list fed page by page, as the server's limit/offset lists are. */
class PagedViewModel<T>(private val pageSize: Int = 60, private val fetch: suspend (limit: Int, offset: Int) -> Page<T>) : ViewModel() {
    private val _state = MutableStateFlow(PagedState<T>())
    val state: StateFlow<PagedState<T>> = _state.asStateFlow()
    private var job: Job? = null

    init {
        loadMore()
    }

    fun loadMore() {
        val s = _state.value
        if (s.loading || s.endReached) return
        _state.value = s.copy(loading = true, error = null)
        job = viewModelScope.launch {
            when (val r = load { fetch(pageSize, s.items.size) }) {
                is Load.Ready -> _state.value = _state.value.copy(items = _state.value.items + r.value.items, total = r.value.total, loading = false)
                is Load.Failed -> _state.value = _state.value.copy(loading = false, error = r.message)
                Load.Loading -> {}
            }
        }
    }

    fun reload() {
        job?.cancel()
        _state.value = PagedState()
        loadMore()
    }
}

@Composable
fun <T> rememberPaged(key: String, pageSize: Int = 60, fetch: suspend (limit: Int, offset: Int) -> Page<T>): PagedViewModel<T> =
    viewModel(key = key) { PagedViewModel(pageSize, fetch) }
