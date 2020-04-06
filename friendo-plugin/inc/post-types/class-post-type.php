<?php

namespace Friendo\PostTypes;

abstract class PostType {

    /**
     * Get slug
     *
     * Get post type slug (ex: book)
     * 
     * @return string
     **/
    abstract protected function getSlug();

    /**
     * Get singular name
     *
     * Get post type singular name (ex: Book)
     * 
     * @return string
     **/
    abstract protected function getSingularName();

    /**
     * Get plural name
     *
     * Get post type plural name (ex: Books);
     * 
     * @return string
     **/
    abstract protected function getPluralName();

    /**
     * Register post type
     *
     * Register's post type with WordPress
     *
     **/
    final public function registerPostType ()
    {
        $labels = [
            'name'                  => _x( $this->getPluralName(), 'Post type general name', 'textdomain' ),
            'singular_name'         => _x( $this->getSingularName(), 'Post type singular name', 'textdomain' ),
            'menu_name'             => _x( $this->getPluralName(), 'Admin Menu text', 'textdomain' ),
            'add_new'               => __( 'Add New', 'textdomain' ),
            'add_new_item'          => __( "Add New {$this->getSingularName()}", 'textdomain' ),
        ];
 
        $args = [
            'labels'             => $labels,
            'public'             => true,
            'publicly_queryable' => true,
            'show_ui'            => true,
            'show_in_menu'       => true,
            'query_var'          => true,
            'rewrite'            => array( 'slug' => $this->getSlug() ),
            'capability_type'    => 'post',
            'has_archive'        => true,
            'hierarchical'       => false,
            'menu_position'      => null,
            'show_in_rest'       => true,
            'supports'           => [ 'title' ],
        ];
        register_post_type( $this->getSlug(), $args );
    }

    /**
     * Load post type
     *
     * Hooks into WordPress init action to load post type
     * 
     **/
    final public function loadPostType ()
    {
        add_action( 'init', [$this, 'registerPostType'] );
    }
}