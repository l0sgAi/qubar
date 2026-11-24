<template>
  <div class="login-page">
    <!-- 动态背景 -->
    <AnimatedBackground />

    <LoginCard />
  </div>
</template>

<script setup>
import AnimatedBackground from '@/components/auth/AnimatedBackground.vue'
import LoginCard from '@/components/auth/LoginCard.vue'
</script>

<style scoped>
.login-page {
  min-height: 100vh;
  background-color: rgb(0, 0, 0);
  display: flex;
  justify-content: center;
  align-items: center;
  position: relative;
  padding: 24px 16px;
}
</style>
