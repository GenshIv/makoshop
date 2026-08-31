<script setup>
import { ref, onMounted, computed } from 'vue';
import { useI18n } from 'vue-i18n';
import { useAuthStore } from '../stores/auth';
import api from '../api';
import { useToast } from '../composables/useToast';

const props = defineProps({
  targetType: { type: String, required: true }, // "product", "category", "eanpage"
  targetId: { type: Number, required: true },
  sortBy: { type: String, default: 'newest' }, // "newest", "oldest", "popular"
});

const { t } = useI18n();
const { user } = useAuthStore();
const { toast } = useToast();

const comments = ref([]);
const total = ref(0);
const page = ref(1);
const limit = ref(20);
const loading = ref(false);
const submitting = ref(false);
const newComment = ref('');
const userVotes = ref({}); // { commentId: 'like' | 'dislike' | null }

const statusOptions = [
  { value: '', label: t('comments.all', 'All') },
  { value: 'approved', label: t('comments.approved', 'Approved') },
  { value: 'pending', label: t('comments.pending', 'Pending') },
];

const totalPages = computed(() => Math.ceil(total.value / limit.value));

const fetchComments = async () => {
  loading.value = true;
  try {
    const params = {
      target_type: props.targetType,
      target_id: props.targetId,
      page: page.value,
      limit: limit.value,
    };
    const res = await api.get('/comments', { params });
    comments.value = res.data.items || [];
    total.value = res.data.total || 0;
    // Reset user votes
    userVotes.value = {};
    // Fetch user votes for each comment
    if (user.value) {
      for (const c of comments.value) {
        try {
          const voteRes = await api.get('/votes/check', {
            params: { target_type: 'comment', target_id: c.id }
          });
          userVotes.value[c.id] = voteRes.data.vote_type || null;
        } catch {
          // Ignore
        }
      }
    }
  } catch (e) {
    console.error('Failed to fetch comments:', e);
    toast.error(t('comments.fetch_error', 'Failed to load comments'));
  } finally {
    loading.value = false;
  }
};

const submitComment = async () => {
  if (!newComment.value.trim()) {
    toast.error(t('comments.comment_required', 'Please enter a comment'));
    return;
  }
  submitting.value = true;
  try {
    await api.post('/comments', {
      target_type: props.targetType,
      target_id: props.targetId,
      content: newComment.value.trim(),
    });
    newComment.value = '';
    await fetchComments();
    toast.success(t('comments.comment_added', 'Comment added'));
  } catch (e) {
    console.error('Failed to submit comment:', e);
    toast.error(t('comments.submit_error', 'Failed to add comment'));
  } finally {
    submitting.value = false;
  }
};

const vote = async (commentId, voteType) => {
  if (!user.value) {
    toast.error(t('comments.login_first', 'Login first'));
    return;
  }
  try {
    const res = await api.post('/votes', {
      target_type: 'comment',
      target_id: commentId,
      vote_type: voteType,
    });
    userVotes.value[commentId] = res.data.vote_type;
    await fetchComments();
  } catch (e) {
    console.error('Vote failed:', e);
    toast.error(t('comments.vote_error', 'Failed to vote'));
  }
};

const formatDate = (ts) => {
  if (!ts) return '';
  return new Date(ts * 1000).toLocaleDateString();
};

onMounted(() => {
  fetchComments();
});
</script>

<template>
  <div class="space-y-6">
    <!-- Comment form -->
    <div v-if="user" class="bg-surface rounded-xl border border-line p-4">
      <h3 class="text-lg font-semibold text-ink mb-3">{{ t('comments.add_title', 'Add a comment') }}</h3>
      <textarea
        v-model="newComment"
        :placeholder="t('comments.placeholder', 'Share your thoughts...')"
        rows="3"
        class="w-full px-3 py-2 border border-line rounded-lg text-sm bg-surface-2 resize-y"
        :maxlength="5000"
      />
      <div class="flex justify-between items-center mt-2">
        <span class="text-xs text-ink-3">{{ newComment.length }}/5000</span>
        <button
          @click="submitComment"
          :disabled="submitting || !newComment.trim()"
          class="btn btn-primary btn-sm"
        >
          {{ submitting ? t('common.sending', 'Sending...') : t('comments.submit', 'Post comment') }}
        </button>
      </div>
    </div>
    <div v-else class="bg-surface rounded-xl border border-line p-4 text-center">
      <p class="text-sm text-ink-2">{{ t('comments.login_to_comment', 'Login to add a comment') }}</p>
    </div>

    <!-- Comments list -->
    <div class="space-y-4">
      <div v-if="loading" class="text-center text-ink-3 py-8">
        {{ t('common.loading', 'Loading...') }}
      </div>
      <div v-else-if="comments.length === 0" class="text-center text-ink-3 py-8">
        {{ t('comments.no_comments', 'No comments yet. Be the first!') }}
      </div>
      <div v-else>
        <div
          v-for="comment in comments"
          :key="comment.id"
          class="bg-surface rounded-xl border border-line p-4"
        >
          <!-- Comment header -->
          <div class="flex items-center gap-2 mb-2">
            <div class="w-8 h-8 rounded-full bg-accent/20 flex items-center justify-center text-accent text-sm font-bold">
              {{ comment.user_name?.charAt(0)?.toUpperCase() || '#' }}
            </div>
            <div>
              <span class="text-sm font-medium text-ink">{{ comment.user_name || `#${comment.user_id}` }}</span>
              <span class="text-xs text-ink-3 ml-2">{{ formatDate(comment.created_at) }}</span>
            </div>
            <span
              v-if="comment.is_featured"
              class="ml-auto px-2 py-0.5 rounded-full text-xs bg-yellow-100 text-yellow-800"
            >
              {{ t('comments.featured', 'Featured') }}
            </span>
          </div>

          <!-- Comment content -->
          <div class="text-sm text-ink-2 mb-3">{{ comment.content }}</div>

          <!-- Like/Dislike -->
          <div class="flex items-center gap-3">
            <button
              @click="vote(comment.id, 'like')"
              class="flex items-center gap-1 text-sm transition-colors"
              :class="userVotes[comment.id] === 'like' ? 'text-green-600' : 'text-ink-3 hover:text-green-600'"
            >
              <span>👍</span>
              <span>{{ comment.like_count || 0 }}</span>
            </button>
            <button
              @click="vote(comment.id, 'dislike')"
              class="flex items-center gap-1 text-sm transition-colors"
              :class="userVotes[comment.id] === 'dislike' ? 'text-red-600' : 'text-ink-3 hover:text-red-600'"
            >
              <span>👎</span>
              <span>{{ comment.dislike_count || 0 }}</span>
            </button>
          </div>
        </div>

        <!-- Pagination -->
        <div v-if="totalPages > 1" class="flex justify-center gap-2 mt-4">
          <button
            @click="page--"
            :disabled="page <= 1"
            class="btn btn-secondary btn-sm"
          >
            {{ t('common.back', 'Back') }}
          </button>
          <span class="px-3 py-1.5 text-sm text-ink-3">
            {{ page }} / {{ totalPages }}
          </span>
          <button
            @click="page++"
            :disabled="page >= totalPages"
            class="btn btn-secondary btn-sm"
          >
            {{ t('common.next', 'Next') }}
          </button>
        </div>
      </div>
    </div>
  </div>
</template>
