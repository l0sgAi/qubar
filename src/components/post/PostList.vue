<template>
  <div class="post-list">
    <PostCard
      v-for="post in posts"
      :key="post.postId"
      :post-id="post.postId"
      :circle-id="post.circleId"
      :circle-name="post.circleName"
      :circle-avatar="post.circleAvatar"
      :circle-color="post.circleColor"
      :user-id="post.userId"
      :user-name="post.userName"
      :user-avatar="post.userAvatar"
      :user-color="post.userColor"
      :title="post.title"
      :content="post.content"
      :cover-image="post.coverImage"
      :images="post.images"
      :view-count="post.viewCount"
      :post-time="post.postTime"
      :like-count="post.likeCount"
      :comment-count="post.commentCount"
      :collect-count="post.collectCount"
      :share-count="post.shareCount ?? null"
      :is-shared="!!post.isShared"
      :show-circle="post.showCircle !== false"
    />
  </div>
</template>

<script setup>
import PostCard from '@/components/post/PostCard.vue'

defineProps({
  posts: {
    type: Array,
    default: () => []
  }
})
</script>

<style scoped>
.post-list {
  display: flex;
  flex-direction: column;
}
</style>
